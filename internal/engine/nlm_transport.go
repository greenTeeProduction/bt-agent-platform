package engine

import (
	"encoding/json"
	"strings"
)

// Retry only passive operations. A timeout after create/query/import can mean
// the server accepted it; replay would duplicate artifacts or consume quota.
func nlmRetrySafe(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] + " " + args[1] {
	case "login --check", "notebook list", "notebook get", "source get", "source fulltext", "research status", "studio status":
		return true
	default:
		return false
	}
}

func nlmResponseFailed(out string) bool {
	if strings.TrimSpace(out) == "" {
		return true
	}
	var value map[string]json.RawMessage
	if json.Unmarshal([]byte(out), &value) != nil {
		return false
	}
	var status string
	_ = json.Unmarshal(value["status"], &status)
	if status == "error" || status == "failed" {
		return true
	}
	if err, ok := value["error"]; ok {
		return string(err) != "null" && string(err) != `""`
	}
	return false
}
