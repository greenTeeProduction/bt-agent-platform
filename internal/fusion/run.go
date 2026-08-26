package fusion

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

type ModelCaller interface {
	GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error)
}

// fusionBreaker short-circuits RunPanel after repeated all-models-failed
// panel runs so a dead model backend isn't re-dispatched to (and re-timed-out
// on) every call.
var fusionBreaker = reliability.NewCircuitBreaker("fusion-panel", 3, 60*time.Second)

// fusionPostPanelRetryPolicy returns a fresh full-jitter retry policy for the
// single-shot Judge/Synthesize LLM calls. Unlike RunPanel (which tolerates
// individual model failures via successfulResponses), Judge and Synthesize
// each make exactly one LLM call; without a retry, a lone transient failure
// after a successful panel run would discard the whole result.
// RetryUnknown is enabled because these calls surface raw upstream errors
// that predate error-category classification, matching the scheduler's
// legacy-compatible retry behavior (cmd/bt-agent/main.go).
func fusionPostPanelRetryPolicy() *reliability.RetryPolicy {
	return &reliability.RetryPolicy{
		MaxRetries:   3,
		Base:         200 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		RetryUnknown: true,
		Jitter:       reliability.FullJitterStrategy,
	}
}

type Tool interface {
	Name() string
	Description() string
	Call(input string) string
}

type Response struct {
	Model      string `json:"model"`
	Content    string `json:"content"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	ToolCalls  int    `json:"tool_calls,omitzero"`
}

type Analysis struct {
	Consensus       []string        `json:"consensus"`
	Contradictions  []Contradiction `json:"contradictions"`
	PartialCoverage []CoveragePoint `json:"partial_coverage"`
	UniqueInsights  []UniqueInsight `json:"unique_insights"`
	BlindSpots      []string        `json:"blind_spots"`
}

type Contradiction struct {
	Topic   string   `json:"topic"`
	Stances []Stance `json:"stances"`
}

type Stance struct {
	Model  string `json:"model"`
	Stance string `json:"stance"`
}

type CoveragePoint struct {
	Models []string `json:"models"`
	Point  string   `json:"point"`
}

type UniqueInsight struct {
	Model   string `json:"model"`
	Insight string `json:"insight"`
}

type Result struct {
	Status    string     `json:"status"`
	Analysis  Analysis   `json:"analysis"`
	Responses []Response `json:"responses"`
	Final     string     `json:"final,omitempty"`
}

func Run(ctx context.Context, caller ModelCaller, cfg Config, prompt string, tools []Tool) (Result, error) {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return Result{Status: "error"}, err
	}
	if !cfg.Enabled {
		return Result{Status: "disabled"}, fmt.Errorf("fusion disabled")
	}

	panelCtx, panelCancel := context.WithTimeout(ctx, cfg.Timeout)
	defer panelCancel()
	responses, err := RunPanel(panelCtx, caller, cfg, prompt, tools)
	if err != nil {
		return Result{Status: "error", Responses: responses}, err
	}

	// Judge and Synthesize each get their own cfg.Timeout budget derived from
	// the original caller ctx, not the panel's already-shrunk context — a slow
	// RunPanel stage must not starve their own retry policies.
	judgeCtx, judgeCancel := context.WithTimeout(ctx, cfg.Timeout)
	defer judgeCancel()
	analysis, err := Judge(judgeCtx, caller, cfg, prompt, successfulResponses(responses))
	if err != nil {
		return Result{Status: "error", Responses: responses}, err
	}
	result := Result{Status: "ok", Analysis: analysis, Responses: responses}

	synthCtx, synthCancel := context.WithTimeout(ctx, cfg.Timeout)
	defer synthCancel()
	final, err := Synthesize(synthCtx, caller, cfg, prompt, result)
	if err != nil {
		final = fallbackFinal(prompt, result)
	}
	result.Final = final
	return result, nil
}

func RunPanel(ctx context.Context, caller ModelCaller, cfg Config, prompt string, tools []Tool) ([]Response, error) {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !fusionBreaker.Allow() {
		return nil, fmt.Errorf("fusion panel: circuit breaker open")
	}
	responses := make([]Response, len(cfg.AnalysisModels))
	var wg sync.WaitGroup
	for i, model := range cfg.AnalysisModels {
		wg.Add(1)
		start := time.Now()
		reliability.SafeGo(fmt.Sprintf("fusion.RunPanel[%s]", model), func() {
			system := panelSystemPrompt(tools)
			content, calls, err := RunToolLoop(ctx, caller, model, system, prompt, tools, cfg)
			responses[i] = Response{Model: model, Content: content, DurationMS: time.Since(start).Milliseconds(), ToolCalls: calls}
			if err != nil {
				responses[i].Error = err.Error()
			}
			wg.Done()
		}, func(panicVal any, panicCtx string) {
			reliability.DefaultPanicHandler(panicVal, panicCtx)
			responses[i] = Response{Model: model, Error: fmt.Sprintf("panic: %v", panicVal), DurationMS: time.Since(start).Milliseconds()}
			wg.Done()
		})
	}
	wg.Wait()
	if len(successfulResponses(responses)) == 0 {
		fusionBreaker.RecordFailure()
		return responses, fmt.Errorf("all fusion panel models failed")
	}
	fusionBreaker.RecordSuccess()
	return responses, nil
}

func Judge(ctx context.Context, caller ModelCaller, cfg Config, prompt string, responses []Response) (Analysis, error) {
	cfg = cfg.Normalize()
	body, _ := json.MarshalIndent(responses, "", "  ")
	judgePrompt := fmt.Sprintf(`You are the Fusion judge. Compare panel responses; do not merge them.
Return ONLY JSON with keys: consensus, contradictions, partial_coverage, unique_insights, blind_spots.

Original prompt:
%s

Panel responses:
%s`, prompt, string(body))
	var raw string
	err := fusionPostPanelRetryPolicy().ExecuteContext(ctx, func() error {
		out, _, callErr := RunToolLoop(ctx, caller, cfg.JudgeModel, judgeSystemPrompt(), judgePrompt, nil, cfg)
		if callErr != nil {
			return callErr
		}
		raw = out
		return nil
	})
	if err != nil {
		return Analysis{}, err
	}
	return parseAnalysisJSON(raw)
}

func Synthesize(ctx context.Context, caller ModelCaller, cfg Config, originalPrompt string, result Result) (string, error) {
	payload, _ := json.MarshalIndent(result, "", "  ")
	prompt := fmt.Sprintf(`Write the final answer using the Fusion judge analysis and raw panel responses.
Treat consensus as higher confidence. Explicitly call out contradictions, uncertainty, useful unique insights, and relevant blind spots. Do not fabricate citations.

Original prompt:
%s

Fusion result:
%s`, originalPrompt, string(payload))
	var final string
	err := fusionPostPanelRetryPolicy().ExecuteContext(ctx, func() error {
		out, callErr := caller.GenerateWithModel(ctx, cfg.JudgeModel, "You synthesize final answers from multi-model deliberation analysis.", prompt)
		if callErr != nil {
			return callErr
		}
		final = out
		return nil
	})
	return final, err
}

func RunToolLoop(ctx context.Context, caller ModelCaller, model, system, prompt string, tools []Tool, cfg Config) (string, int, error) {
	if len(tools) == 0 {
		out, err := caller.GenerateWithModel(ctx, model, system+"\nWeb tools unavailable in this run; state uncertainty explicitly.", prompt)
		return out, 0, err
	}
	scratch := ""
	toolMap := map[string]Tool{}
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	for i := 0; i < cfg.MaxToolCalls; i++ {
		full := prompt + "\n\nPrevious tool observations:\n" + scratch + "\nRespond with Action/Action Input or Final Answer."
		out, err := caller.GenerateWithModel(ctx, model, system, full)
		if err != nil {
			return scratch, i, err
		}
		if final := parseFinalAnswer(out); final != "" {
			return final, i, nil
		}
		action, input := parseAction(out)
		if action == "" {
			return out, i, nil
		}
		tool, ok := toolMap[action]
		if !ok {
			scratch += fmt.Sprintf("Action %s unavailable.\n", action)
			continue
		}
		obs := tool.Call(input)
		scratch += fmt.Sprintf("Action: %s(%s) -> %s\n", action, input, obs)
	}
	return strings.TrimSpace(scratch), cfg.MaxToolCalls, nil
}

func parseAnalysisJSON(raw string) (Analysis, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimSpace(strings.Trim(raw, "`"))
		raw = strings.TrimPrefix(raw, "json")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var analysis Analysis
	if err := json.Unmarshal([]byte(raw), &analysis); err != nil {
		return Analysis{}, fmt.Errorf("parse fusion judge JSON: %w", err)
	}
	return analysis, nil
}

func successfulResponses(responses []Response) []Response {
	out := make([]Response, 0, len(responses))
	for _, r := range responses {
		if r.Error == "" && strings.TrimSpace(r.Content) != "" {
			out = append(out, r)
		}
	}
	return out
}

func panelSystemPrompt(tools []Tool) string {
	base := "You are an independent Fusion panel model. Answer the prompt independently, cite uncertainty, identify assumptions, and do not defer to other panelists."
	if len(tools) == 0 {
		return base + " Web tools are unavailable."
	}
	return base + " You may use web_search/web_fetch style tools with Action and Action Input before Final Answer."
}

func judgeSystemPrompt() string {
	return "You are the Fusion judge. Compare responses rather than merging them. Return strict JSON."
}

func parseFinalAnswer(s string) string {
	re := regexp.MustCompile(`(?is)Final Answer:\s*(.*)`)
	m := re.FindStringSubmatch(s)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func parseAction(s string) (string, string) {
	a := regexp.MustCompile(`(?im)^Action:\s*(.+)$`).FindStringSubmatch(s)
	i := regexp.MustCompile(`(?im)^Action Input:\s*(.+)$`).FindStringSubmatch(s)
	if len(a) != 2 || len(i) != 2 {
		return "", ""
	}
	return strings.TrimSpace(a[1]), strings.TrimSpace(i[1])
}

func fallbackFinal(prompt string, result Result) string {
	b, _ := json.MarshalIndent(result.Analysis, "", "  ")
	return fmt.Sprintf("Fusion analysis for prompt %q:\n%s", prompt, string(b))
}
