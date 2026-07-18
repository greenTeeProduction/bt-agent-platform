package a2a

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// BTAgentClient is an A2A client that BT trees use to delegate to external agents.
type BTAgentClient struct {
	Timeout time.Duration
}

// sendTaskRetries/sendTaskBaseDelay/sendTaskMaxDelay bound how hard SendTask
// retries a transient client.SendMessage failure before giving up on the
// delegation — the same shape as RunAuction's winner-dispatch retry
// (auction.go:376-397) one level down, at the raw transport call every
// dispatch (auction winner or otherwise) ultimately goes through. Unlike the
// winner dispatch retry, delays here are jittered (JitterStrategy) so that a
// burst of SendTask calls racing the same flaky agent don't all retry in
// lockstep. Non-retryable categories (validation, auth) fail immediately
// regardless of these bounds.
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
	return &BTAgentClient{Timeout: 120 * time.Second}
}

// SendTask delegates a task to an external A2A agent.
// agentURL is the A2A server base URL (e.g., "http://agent.example.com:8001").
// taskText is the plain-text task to send.
// Returns the agent's text response or an error.
func (c *BTAgentClient) SendTask(ctx context.Context, agentURL, taskText string) (string, error) {
	// Resolve agent card
	card, err := agentcard.DefaultResolver.Resolve(ctx, agentURL)
	if err != nil {
		return "", fmt.Errorf("resolve agent card at %s: %w", agentURL, err)
	}

	// Create client from card
	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return "", fmt.Errorf("create A2A client: %w", err)
	}

	// Build and send message
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(taskText))
	req := &a2a.SendMessageRequest{Message: msg}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	policy := &reliability.RetryPolicy{
		MaxRetries: sendTaskRetries,
		Base:       sendTaskBaseDelay,
		MaxDelay:   sendTaskMaxDelay,
		Jitter:     reliability.FullJitterStrategy,
	}
	var resp a2a.SendMessageResult
	sendErr := policy.ExecuteContext(timeoutCtx, func() error {
		var err error
		resp, err = client.SendMessage(timeoutCtx, req)
		return err
	})
	if sendErr != nil {
		return "", fmt.Errorf("send message: %w", sendErr)
	}

	return interpretSendResult(resp)
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
		for _, part := range r.Parts {
			if t := part.Text(); t != "" {
				return t, nil
			}
		}
		return "", fmt.Errorf("a2a: agent message carried no text content")
	case *a2a.Task:
		if r.Status.State != a2a.TaskStateCompleted {
			return "", fmt.Errorf("a2a: task %s did not complete: state=%s status=%s", r.ID, r.Status.State, safetyGetMessageText(r.Status.Message))
		}
		for _, artifact := range r.Artifacts {
			for _, part := range artifact.Parts {
				if t := part.Text(); t != "" {
					return t, nil
				}
			}
		}
		return "", fmt.Errorf("a2a: task %s completed with no artifact text", r.ID)
	default:
		return "", fmt.Errorf("a2a: agent returned unrecognized response type %T", resp)
	}
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
