package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

const bbReadMaxDisplay = 4000

// PrepareBlackboard initializes ChainState metadata and attaches bb_* ReAct tools.
func PrepareBlackboard(bb *Blackboard) {
	if bb == nil || bb.BB == nil {
		return
	}
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	bb.ChainState["run_id"] = bb.BB.RunID
	if bb.BB.AgentName != "" {
		bb.ChainState["agent_name"] = bb.BB.AgentName
	}
	if bb.BB.SessionID != "" {
		bb.ChainState["session_id"] = bb.BB.SessionID
	}
	attachBlackboardTools(bb)
}

func attachBlackboardTools(bb *Blackboard) {
	if bb == nil || bb.BB == nil {
		return
	}
	hasRunTools := false
	hasSessionTools := false
	for _, t := range bb.ChainTools {
		if n, ok := t.(interface{ Name() string }); ok {
			switch n.Name() {
			case "bb_read", "bb_write", "bb_list", "bb_append":
				hasRunTools = true
			case "bb_session_read", "bb_session_write", "bb_session_list", "bb_session_append":
				hasSessionTools = true
			}
		}
	}
	if hasRunTools && (bb.BB.SessionID == "" || hasSessionTools) {
		return
	}
	h := bb.BB
	if !hasRunTools {
		bb.ChainTools = append(bb.ChainTools,
			&bbTool{name: "bb_read", description: "Read a value from the run blackboard by key. Action Input: key (e.g. work/notes).", handle: h, kind: "read"},
			&bbTool{name: "bb_write", description: "Write a value to the run blackboard. Action Input: JSON {\"key\":\"work/x\",\"value\":\"...\",\"summary\":\"optional\"}.", handle: h, kind: "write"},
			&bbTool{name: "bb_list", description: "List blackboard keys in the run scope. Action Input: optional key prefix.", handle: h, kind: "list"},
			&bbTool{name: "bb_append", description: "Append a line to a run blackboard key, creating it if absent (good for accumulating task history or subtask results). Action Input: JSON {\"key\":\"work/log\",\"value\":\"...\"}.", handle: h, kind: "append"},
		)
	}
	if h.SessionID != "" && !hasSessionTools {
		bb.ChainTools = append(bb.ChainTools,
			&bbTool{name: "bb_session_read", description: "Read a value from the pipeline session blackboard (shared across workflow steps). Action Input: key (e.g. steps/analyze/output).", handle: h, kind: "session_read"},
			&bbTool{name: "bb_session_write", description: "Write to the pipeline session blackboard. Action Input: JSON {\"key\":\"work/x\",\"value\":\"...\",\"summary\":\"optional\"}.", handle: h, kind: "session_write"},
			&bbTool{name: "bb_session_list", description: "List keys in the pipeline session blackboard. Action Input: optional key prefix.", handle: h, kind: "session_list"},
			&bbTool{name: "bb_session_append", description: "Append a line to a session blackboard key shared across workflow steps (good for accumulating cross-step results). Action Input: JSON {\"key\":\"steps/log\",\"value\":\"...\"}.", handle: h, kind: "session_append"},
		)
	}
}

type bbTool struct {
	name        string
	description string
	handle      *blackboard.Handle
	kind        string
}

func (t *bbTool) Name() string        { return t.name }
func (t *bbTool) Description() string { return t.description }

func (t *bbTool) Call(input string) string {
	switch t.kind {
	case "read":
		return bbToolRead(t.handle, input)
	case "write":
		return bbToolWrite(t.handle, input)
	case "list":
		return bbToolList(t.handle, input)
	case "append":
		return bbToolAppend(t.handle, input)
	case "session_append":
		return bbToolSessionAppend(t.handle, input)
	case "session_read":
		return bbToolSessionRead(t.handle, input)
	case "session_write":
		return bbToolSessionWrite(t.handle, input)
	case "session_list":
		return bbToolSessionList(t.handle, input)
	default:
		return "unknown bb tool kind"
	}
}

func bbToolRead(h *blackboard.Handle, input string) string {
	key := strings.TrimSpace(input)
	if key == "" {
		return "error: key required"
	}
	e, err := h.Get(key)
	if err != nil {
		return "error: " + err.Error()
	}
	return formatBBEntry(e)
}

func bbToolWrite(h *blackboard.Handle, input string) string {
	key, value, summary, err := parseBBWriteInput(input)
	if err != nil {
		return "error: " + err.Error()
	}
	if err := h.Set(key, value, summary, "text"); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("stored key=%s bytes=%d", key, len(value))
}

func bbToolList(h *blackboard.Handle, input string) string {
	entries, err := h.List(strings.TrimSpace(input), 50)
	if err != nil {
		return "error: " + err.Error()
	}
	return formatBBEntries(entries)
}

func bbToolAppend(h *blackboard.Handle, input string) string {
	if h == nil || h.Mgr == nil || h.RunID == "" {
		return "error: blackboard handle not configured"
	}
	key, value, _, err := parseBBWriteInput(input)
	if err != nil {
		return "error: " + err.Error()
	}
	e, err := h.Mgr.Append(h.RunScope(), key, value, "\n", "text")
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("appended key=%s bytes=%d total=%d", key, len(value), len(e.Value))
}

func bbToolSessionAppend(h *blackboard.Handle, input string) string {
	if h == nil || h.Mgr == nil {
		return "error: blackboard handle not configured"
	}
	if h.SessionID == "" {
		return "error: session scope not configured"
	}
	key, value, _, err := parseBBWriteInput(input)
	if err != nil {
		return "error: " + err.Error()
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeSession, ID: h.SessionID}
	e, err := h.Mgr.Append(scope, key, value, "\n", "text")
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("appended session key=%s bytes=%d total=%d", key, len(value), len(e.Value))
}

func bbToolSessionRead(h *blackboard.Handle, input string) string {
	key := strings.TrimSpace(input)
	if key == "" {
		return "error: key required"
	}
	e, err := h.GetSession(key)
	if err != nil {
		return "error: " + err.Error()
	}
	return formatBBEntry(e)
}

func bbToolSessionWrite(h *blackboard.Handle, input string) string {
	key, value, summary, err := parseBBWriteInput(input)
	if err != nil {
		return "error: " + err.Error()
	}
	if err := h.SetSession(key, value, summary, "text"); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("stored session key=%s bytes=%d", key, len(value))
}

func bbToolSessionList(h *blackboard.Handle, input string) string {
	entries, err := h.ListSession(strings.TrimSpace(input), 50)
	if err != nil {
		return "error: " + err.Error()
	}
	return formatBBEntries(entries)
}

func formatBBEntry(e blackboard.Entry) string {
	if len(e.Value) > bbReadMaxDisplay {
		return fmt.Sprintf("key=%s summary=%s value=%s... [truncated, %d bytes total]",
			e.Key, e.Summary, e.Value[:bbReadMaxDisplay], len(e.Value))
	}
	if e.Summary != "" && e.Summary != e.Value {
		return fmt.Sprintf("key=%s summary=%s value=%s", e.Key, e.Summary, e.Value)
	}
	return e.Value
}

func formatBBEntries(entries []blackboard.Entry) string {
	if len(entries) == 0 {
		return "(no keys)"
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "- %s (%d bytes) summary=%s\n", e.Key, e.SizeBytes, e.Summary)
	}
	return strings.TrimSpace(b.String())
}

func parseBBWriteInput(input string) (key, value, summary string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", "", fmt.Errorf("empty input")
	}
	if strings.HasPrefix(input, "{") {
		var req struct {
			Key     string `json:"key"`
			Value   string `json:"value"`
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(input), &req); err != nil {
			return "", "", "", err
		}
		if strings.TrimSpace(req.Key) == "" {
			return "", "", "", fmt.Errorf("key required")
		}
		return req.Key, req.Value, req.Summary, nil
	}
	// key=value fallback for simple writes
	k, v, ok := strings.Cut(input, "=")
	if !ok {
		return "", "", "", fmt.Errorf("expected JSON or key=value")
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), "", nil
}
