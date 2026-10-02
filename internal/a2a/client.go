package a2a

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// BTAgentClient is an A2A client that BT trees use to delegate to external agents.
type BTAgentClient struct {
	APIKey      string
	PlatformURL string
	Timeout     time.Duration
}

// Only explicit rejection before admission permits a SendMessage retry. An
// HTTP failure alone says nothing about whether the remote operation ran.
const (
	sendTaskRetries   = 3
	sendTaskBaseDelay = 50 * time.Millisecond
	sendTaskMaxDelay  = 500 * time.Millisecond
)

// BTAgentClient is the production transport an Auctioneer fans announcements out
// over; its SendTask satisfies BidCollector.
var _ BidCollector = (*BTAgentClient)(nil)

// NewBTAgentClient creates a new A2A client for BT-to-external delegation.
func NewBTAgentClient() *BTAgentClient {
	client := &BTAgentClient{Timeout: 120 * time.Second}
	if cfg := platformClientCredentials.Load(); cfg != nil {
		client.APIKey, client.PlatformURL = cfg.key, cfg.baseURL
	}
	return client
}

// SendTask delegates a task to an external A2A agent.
// agentURL is the A2A server base URL (e.g., "http://agent.example.com:8001").
// taskText is the plain-text task to send.
// Returns the agent's text response or an error.
func (c *BTAgentClient) SendTask(ctx context.Context, agentURL, taskText string) (string, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := timeoutCtx.Err(); err != nil {
		return "", err // no request was dispatched
	}
	// Resolve agent card
	card, err := agentcard.DefaultResolver.Resolve(timeoutCtx, agentURL)
	if err != nil {
		return "", fmt.Errorf("resolve agent card at %s: %w", agentURL, err)
	}

	// Create client from card
	transport := &platformKeyTransport{key: c.APIKey, platformURL: c.PlatformURL, sourceURL: agentURL}
	httpClient := &http.Client{
		Transport: transport,
		// Do not forward credentials through HTTP redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	client, err := a2aclient.NewFromCard(timeoutCtx, card, a2aclient.WithJSONRPCTransport(httpClient), a2aclient.WithRESTTransport(httpClient))
	if err != nil {
		return "", fmt.Errorf("create A2A client: %w", err)
	}

	// Build and send message
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(taskText))
	req := &a2a.SendMessageRequest{Message: msg}

	policy := &reliability.RetryPolicy{
		MaxRetries: sendTaskRetries,
		Base:       sendTaskBaseDelay,
		MaxDelay:   sendTaskMaxDelay,
		Jitter:     reliability.FullJitterStrategy,
	}
	var resp a2a.SendMessageResult
	sendErr := policy.ExecuteContext(timeoutCtx, func() error {
		if err := timeoutCtx.Err(); err != nil {
			return reliability.NewCategorizedError(reliability.ErrCatValidation, err)
		}
		transport.notAdmitted.Store(false)
		var err error
		resp, err = client.SendMessage(timeoutCtx, req)
		if err != nil && !transport.notAdmitted.Load() {
			return &reliability.ExecutionUncertainError{Err: err}
		}
		return err
	})
	if sendErr != nil {
		return "", fmt.Errorf("send message: %w", sendErr)
	}

	// A submitted/working task already has an execution owner. Poll that task,
	// never send a new message, until it completes, pauses, or consumes the
	// original caller budget. The SDK intentionally detaches asynchronous work
	// from the HTTP request; a polling deadline is not task cancellation.
	for {
		task, ok := resp.(*a2a.Task)
		if !ok || task == nil || (task.Status.State != a2a.TaskStateSubmitted && task.Status.State != a2a.TaskStateWorking) {
			return interpretSendResult(resp)
		}
		if task.ID == "" {
			return "", uncertainResponse("active task has no ID")
		}
		if err := waitForTaskPoll(timeoutCtx); err != nil {
			return "", &reliability.ExecutionUncertainError{Err: fmt.Errorf("poll task %s: %w", task.ID, err)}
		}
		next, err := client.GetTask(timeoutCtx, &a2a.GetTaskRequest{ID: task.ID})
		if err != nil {
			return "", &reliability.ExecutionUncertainError{Err: fmt.Errorf("poll task %s: %w", task.ID, err)}
		}
		if next == nil || next.ID != task.ID || next.ContextID != task.ContextID {
			return "", uncertainResponse("poll returned a different or missing task")
		}
		resp = next
	}
}

func waitForTaskPoll(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func uncertainResponse(detail string) error {
	return &reliability.ExecutionUncertainError{Err: fmt.Errorf("a2a: %s", detail)}
}

// interpretSendResult extracts the honest outcome of a SendMessage call: it
// returns the agent's text response only when the delegated work actually
// produced one, and a non-nil error in every other case — a task that did not
// complete (failed, canceled, rejected, or any other non-completed state) or a
// message that carries no content is never laundered into a success string
// describing the failure, since callers (including the auction winner
// dispatch in auction.go) rely on a non-nil error to detect and react to a
// failed delegation.
func interpretSendResult(resp a2a.SendMessageResult) (string, error) {
	switch r := resp.(type) {
	case *a2a.Message:
		if r == nil {
			return "", uncertainResponse("missing agent message")
		}
		for _, part := range r.Parts {
			if t := part.Text(); t != "" {
				return t, nil
			}
		}
		return "", uncertainResponse("agent message carried no text content")
	case *a2a.Task:
		if r == nil {
			return "", uncertainResponse("missing task")
		}
		diagnostic := taskExecutionDiagnostic(r)
		if reliability.IsExecutionUncertainError(diagnostic) {
			return taskResponseEvidence(r), diagnostic
		}
		if r.Status.State != a2a.TaskStateCompleted {
			if !r.Status.State.Terminal() && r.Status.State != a2a.TaskStateInputRequired && r.Status.State != a2a.TaskStateAuthRequired {
				return "", uncertainResponse(fmt.Sprintf("task %s has unrecognized state %s", r.ID, r.Status.State))
			}
			// A failed/canceled/paused task is evidence, not a transport failure
			// whose status text (possibly containing "timeout") warrants replay.
			return taskResponseEvidence(r), stoppedTaskDiagnostic(r, diagnostic)
		}
		if reliability.IsExecutionStoppedError(diagnostic) {
			return taskResponseEvidence(r), uncertainResponse("completed task carries stopped diagnostic")
		}
		for _, artifact := range r.Artifacts {
			if artifact == nil {
				continue
			}
			for _, part := range artifact.Parts {
				if t := part.Text(); t != "" {
					return t, diagnostic
				}
			}
		}
		return "", uncertainResponse(fmt.Sprintf("task %s completed with no artifact text%s", r.ID, diagnosticSuffix(diagnostic)))
	default:
		return "", uncertainResponse(fmt.Sprintf("agent returned unrecognized response type %T", resp))
	}
}

// Preserve received evidence even when the outcome itself remains unknown.
func taskResponseEvidence(task *a2a.Task) string {
	for _, artifact := range task.Artifacts {
		if artifact == nil {
			continue
		}
		for _, part := range artifact.Parts {
			if text := part.Text(); text != "" {
				return text
			}
		}
	}
	if task.Status.Message != nil {
		return safetyGetMessageText(task.Status.Message)
	}
	return ""
}

// DiscoverAgents resolves the agent card and returns it.
func (c *BTAgentClient) DiscoverAgents(ctx context.Context, agentURL string) (*a2a.AgentCard, error) {
	return agentcard.DefaultResolver.Resolve(ctx, agentURL)
}

func safetyGetMessageText(msg *a2a.Message) string {
	if msg == nil {
		return "no status message"
	}
	for _, part := range msg.Parts {
		if t := part.Text(); t != "" {
			return t
		}
	}
	return ""
}

// ConfigurePlatformClient wires the resolved platform credential into built-in
// delegation and auction clients. Call during startup; no secret is generated.
func ConfigurePlatformClient(apiKey, baseURL string) {
	platformClientCredentials.Store(&clientCredentials{key: apiKey, baseURL: baseURL})
}

type clientCredentials struct{ key, baseURL string }

var platformClientCredentials atomic.Pointer[clientCredentials]

type platformKeyTransport struct {
	key, platformURL, sourceURL string
	notAdmitted                 atomic.Bool
}

func sameOrigin(a, b string) bool {
	x, ex := url.Parse(a)
	y, ey := url.Parse(b)
	return ex == nil && ey == nil && x.Host != "" && y.Host != "" && x.User == nil && y.User == nil &&
		(x.Scheme == "http" || x.Scheme == "https") && x.Scheme == y.Scheme && strings.EqualFold(x.Host, y.Host)
}
func (t *platformKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.key != "" && sameOrigin(t.sourceURL, t.platformURL) && sameOrigin(req.URL.String(), t.platformURL) {
		req.Header.Set("X-API-Key", t.key)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil && resp != nil && (resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices) && resp.Header.Get(reliability.ExecutionAdmissionHeader) == "false" && sameOrigin(req.URL.String(), t.sourceURL) {
		t.notAdmitted.Store(true)
	}
	return resp, err
}
