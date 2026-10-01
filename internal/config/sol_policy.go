package config

import (
	"os"
	"strconv"
	"strings"
)

// SolModel is the owner's required model for ordinary BT inference roles (external integrations have explicit exceptions).
const SolModel = "gpt-6.1-sol"

// SolOnly defaults closed. The explicit opt-out exists for isolated legacy
// adapter tests; the deployed services set this policy to true.
func SolOnly() bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("BT_LLM_SOL_ONLY")))
	return err != nil || value
}
