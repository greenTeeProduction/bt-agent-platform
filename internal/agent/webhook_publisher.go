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
}

// NewWebhookPublisher creates a publisher with Hermes webhook base URL and secrets.
func NewWebhookPublisher(baseURL string, secrets WebhookSecrets) *WebhookPublisher {
	return &WebhookPublisher{
		baseURL:  baseURL,
		secrets:  secrets,
		client:   &http.Client{Timeout: 10 * time.Second},
		stopCh:   make(chan struct{}),
		throttle: newRoutineThrottle(NotificationThrottleFile()),
	}
}

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

	secret, ok := p.secrets[subscription]
	if !ok {
		slog.Warn("webhook: no secret for subscription", "subscription", subscription)
		return
	}

	url := fmt.Sprintf("%s/webhooks/%s", p.baseURL, subscription)

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

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Error("webhook: request build error", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// HMAC-SHA256 signature (Hermes expects X-Hub-Signature-256 header)
	sig := computeHMAC(body, secret)
	req.Header.Set("X-Hub-Signature-256", "sha256="+sig)

	resp, err := p.client.Do(req)
	if err != nil {
		slog.Error("webhook: POST failed", "subscription", subscription, "error", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("webhook: POST returned error status", "subscription", subscription, "status", resp.StatusCode)
	}
}

// computeHMAC returns the hex-encoded HMAC-SHA256 signature.
func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
