package a2a

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

// BTAgentClient is an A2A client that BT trees use to delegate to external agents.
type BTAgentClient struct {
	Timeout time.Duration
}

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

	resp, err := client.SendMessage(timeoutCtx, req)
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	// Extract text from response. SendMessageResult is either *Message or *Task.
	switch r := resp.(type) {
	case *a2a.Message:
		for _, part := range r.Parts {
			if t := part.Text(); t != "" {
				return t, nil
			}
		}
		return "A2A agent returned empty message", nil
	case *a2a.Task:
		if r.Status.State == a2a.TaskStateCompleted {
			for _, artifact := range r.Artifacts {
				for _, part := range artifact.Parts {
					if t := part.Text(); t != "" {
						return t, nil
					}
				}
			}
		}
		return fmt.Sprintf("Task %s: state=%s status=%s", r.ID, r.Status.State, safetyGetMessageText(r.Status.Message)), nil
	default:
		return "A2A agent returned unrecognized response type", nil
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
