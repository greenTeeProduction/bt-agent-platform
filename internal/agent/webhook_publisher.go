// Package agent provides the WebhookPublisher — bridges AgentBus events
// to Hermes webhook subscriptions for external notification and agent triggering.
//
// Architecture:
//
//	AgentBus event → WebhookPublisher → HMAC-signed POST → Hermes webhook → agent run
//
// Supported endpoints (configured via Hermes webhook subscribe):
//   - bt-agent-alert     (health/service_down → agent investigation)
//   - bt-task-complete   (task done → Telegram notification, deliver-only)
//   - bt-evolution-event (evolution step → agent analysis)
//
// Usage:
//
//	pub := NewWebhookPublisher("http://localhost:8644", secrets)
//	pub.Attach(agent.GlobalAgentBus)  // subscribes to all events
package agent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// WebhookSecrets maps webhook subscription names to their HMAC secrets.
type WebhookSecrets map[string]string

// DefaultWebhookSecrets returns secrets for the default BT event subscriptions.
// These are created via: hermes webhook subscribe bt-agent-alert ...
func DefaultWebhookSecrets() WebhookSecrets {
	return WebhookSecrets{
		"bt-agent-alert":     "Mm6ohHCFqWa4OZzOYnkMpMl8nA7Lp41K9hy8CsIFQVg",
		"bt-task-complete":   "5IPr_fPHgQIREQyALrpCQJfZhriMX3pzwR1bQOL5MHw",
		"bt-evolution-event": "hXXqMTGXWRT4chKuXHcXc2YvucuWEy5hR7PhIwu9bso",
	}
}

// eventRoute maps AgentBus event types to webhook subscription names.
var eventRoute = map[string]string{
	"service_down":   "bt-agent-alert",
	"health_alert":   "bt-agent-alert",
	"error_detected": "bt-agent-alert",
	"task_complete":  "bt-task-complete",
	"evolution_step": "bt-evolution-event",
}

// WebhookPublisher bridges AgentBus events to Hermes webhooks.
type WebhookPublisher struct {
	baseURL  string
	secrets  WebhookSecrets
	client   *http.Client
	stopCh   chan struct{}
	eventCh  <-chan AgentEvent
	throttle *routineThrottle
	breakers map[string]*reliability.CircuitBreaker
	dlq      *reliability.DeadLetterQueue
}

// NewWebhookPublisher creates a publisher with Hermes webhook base URL and secrets.
func NewWebhookPublisher(baseURL string, secrets WebhookSecrets) *WebhookPublisher {
	breakers := make(map[string]*reliability.CircuitBreaker, len(secrets))
	for subscription := range secrets {
		breakers[subscription] = reliability.NewCircuitBreaker(subscription, webhookCircuitBreakerThreshold, webhookCircuitBreakerCooldown)
	}
	pub := &WebhookPublisher{
		baseURL:  baseURL,
		secrets:  secrets,
		client:   &http.Client{Timeout: 10 * time.Second},
		stopCh:   make(chan struct{}),
		throttle: newRoutineThrottle(NotificationThrottleFile()),
		breakers: breakers,
		// In-memory only: each WebhookPublisher instance owns its own DLQ, and
		// unlike the scheduler's file-backed queue (wired once per daemon in
		// main.go), publishers are constructed per-test as well as per-process,
		// so a shared persistence path would leak dead letters across them.
		dlq: reliability.NewDeadLetterQueue(""),
	}
	// Replays run the delivery ONCE through the same signed-request path used
	// by handleEvent, without webhookRetryPolicy's own retries — a failed
	// replay simply retains the entry (see DeadLetterQueue.Replay) rather than
	// pushing a duplicate dead letter.
	pub.dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {
		_, err := pub.postSigned(e.Agent, []byte(e.Task))
		return err
	})
	return pub
}

// webhookCircuitBreakerThreshold/Cooldown configure the per-subscription
// breaker: five consecutive failed deliveries (each already exhausting
// webhookRetryPolicy's own retries) trip the circuit, which then stays open
// for a minute before allowing a single half-open probe through.
const (
	webhookCircuitBreakerThreshold = 5
	webhookCircuitBreakerCooldown  = time.Minute
)

// Attach subscribes to the AgentBus and starts forwarding events to Hermes webhooks.
// Runs in a goroutine until Close() is called.
func (p *WebhookPublisher) Attach(bus *AgentBus) {
	p.eventCh = bus.Subscribe("") // subscribe to ALL events
	reliability.SafeGo("webhook-publisher-loop", p.loop, nil)
}

// Close stops the publisher goroutine.
func (p *WebhookPublisher) Close() {
	close(p.stopCh)
}

func (p *WebhookPublisher) loop() {
	for {
		select {
		case <-p.stopCh:
			return
		case event, ok := <-p.eventCh:
			if !ok {
				return
			}
			// Recover per-event so a single panicking handler (e.g. a
			// malformed event.Data payload that panics during
			// json.Marshal) doesn't unwind the whole loop and stop
			// forwarding subsequent events.
			_ = reliability.Recover("webhook-publisher-handle-event", func() {
				p.handleEvent(event)
			})
		}
	}
}

func (p *WebhookPublisher) handleEvent(event AgentEvent) {
	subscription, ok := eventRoute[event.Type]
	if !ok {
		// Unknown event type — log and skip
		slog.Warn("webhook: unhandled event type", "type", event.Type, "source", event.Source)
		return
	}

	// Routine-notification throttle: repeat healthy no_change cycles are
	// suppressed and rolled up instead of spamming the operator hourly
	// (13/15 bt-fusion Telegram messages on 2026-07-15 were the identical
	// "0 new findings" no-op). Only task_complete is throttled — alert and
	// evolution routes stay untouched.
	if event.Type == "task_complete" && p.throttle != nil {
		data, _ := event.Data.(map[string]interface{})
		send, annotated := p.throttle.decide(event.Source, eventDataString(data, "outcome"), eventDataString(data, "summary"))
		if !send {
			slog.Info("webhook: routine notification suppressed", "agent", event.Source)
			return
		}
		if annotated != "" {
			// Copy the map — event.Data is shared with other bus subscribers.
			patched := make(map[string]interface{}, len(data)+1)
			for k, v := range data {
				patched[k] = v
			}
			patched["summary"] = annotated
			event.Data = patched
		}
	}

	if _, ok := p.secrets[subscription]; !ok {
		slog.Warn("webhook: no secret for subscription", "subscription", subscription)
		return
	}

	// Build JSON payload matching the webhook prompt template variables
	payload := map[string]interface{}{
		"type":      event.Type,
		"source":    event.Source,
		"message":   event.Message,
		"data":      event.Data,
		"timestamp": event.Timestamp.Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook: marshal error", "error", err)
		return
	}

	// Take the breaker probe only once we're actually about to deliver. Doing
	// this before json.Marshal meant an unmarshalable event.Data consumed the
	// single half-open probe and then returned without recording an outcome,
	// wedging the breaker HalfOpen forever (Allow() then always returns false)
	// so every later deliverable event was silently dropped.
	breaker := p.breakers[subscription]
	if breaker != nil && !breaker.Allow() {
		slog.Warn("webhook: circuit breaker open, skipping delivery", "subscription", subscription)
		return
	}

	var lastStatus int
	err = webhookRetryPolicy().ExecuteContext(context.Background(), func() error {
		status, postErr := p.postSigned(subscription, body)
		lastStatus = status
		return postErr
	})

	if err != nil {
		slog.Warn("webhook: POST failed after retries", "subscription", subscription, "status", lastStatus, "error", err)
		if breaker != nil {
			// Only infrastructure failures (5xx/transport, per postSigned's
			// typed classification) walk the breaker toward open: a payload
			// Hermes rejects with 4xx must not suppress deliverable events for
			// the whole cooldown. A consumed half-open probe is still resolved.
			breaker.RecordOutcome(err)
		}
		p.dlq.Push(reliability.DeadLetterEntry{
			Agent: subscription,
			Task:  string(body),
			Error: err.Error(),
		})
		return
	}

	if breaker != nil {
		breaker.RecordSuccess()
	}
}

// postSigned POSTs body to the given subscription's Hermes webhook endpoint,
// HMAC-signed with that subscription's secret, and returns the response
// status alongside a reliability-categorized error. Shared by handleEvent's
// retrying delivery path and the DLQ's single-shot replay executor so a
// replayed dead letter is re-delivered through the identical signing logic
// the original attempt used.
func (p *WebhookPublisher) postSigned(subscription string, body []byte) (int, error) {
	secret, ok := p.secrets[subscription]
	if !ok {
		return 0, reliability.NewCategorizedError(reliability.ErrCatValidation,
			fmt.Errorf("webhook: no secret for subscription %q", subscription))
	}
	url := fmt.Sprintf("%s/webhooks/%s", p.baseURL, subscription)
	sig := computeHMAC(body, secret)

	req, reqErr := http.NewRequest("POST", url, bytes.NewReader(body))
	if reqErr != nil {
		return 0, reliability.NewCategorizedError(reliability.ErrCatValidation, reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256="+sig)

	resp, doErr := p.client.Do(req)
	if doErr != nil {
		// Transport errors (connection refused/reset, EOF, DNS, timeout) are
		// classified as retryable network errors by ClassifyError.
		return 0, doErr
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 500:
		// Hermes-side trouble — worth retrying.
		return resp.StatusCode, reliability.NewCategorizedError(reliability.ErrCatNetwork,
			fmt.Errorf("webhook: server error status %d", resp.StatusCode))
	case resp.StatusCode >= 400:
		// The request itself is bad — retrying wastes attempts.
		return resp.StatusCode, reliability.NewCategorizedError(reliability.ErrCatValidation,
			fmt.Errorf("webhook: client error status %d", resp.StatusCode))
	default:
		return resp.StatusCode, nil
	}
}

// webhookRetryPolicy returns the reliability.RetryPolicy used for the
// outbound Hermes webhook POST. Transport errors and 5xx responses are
// retried with jittered exponential backoff (FullJitterStrategy — see
// reliability.FullJitter); 4xx responses mean the request itself is bad, so
// RetryPolicy.IsRetryable() refuses to retry them (ErrCatValidation). Bounded
// at 3 attempts / 500ms max delay so a Hermes outage doesn't stall the
// AgentBus subscriber loop for long.
func webhookRetryPolicy() *reliability.RetryPolicy {
	return &reliability.RetryPolicy{
		MaxRetries: 3,
		Base:       50 * time.Millisecond,
		MaxDelay:   500 * time.Millisecond,
		Jitter:     reliability.FullJitterStrategy,
	}
}

// computeHMAC returns the hex-encoded HMAC-SHA256 signature.
func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
