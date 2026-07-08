package main

import (
	"os"
	"strings"
	"testing"
)

// requireBuildIdentityWiring asserts — at the source level, the same way
// TestDaemonPlumbsExperienceBankIntoMCPDeps audits main.go — that a
// long-running binary's main package installs and logs its build identity at
// startup: it must call dashboard.InstallBuildIdentity() (reads
// runtime/debug.ReadBuildInfo, publishes the bt_build_info{revision,dirty}
// gauge, returns the identity) and log all three identity fields
// (vcs_revision, vcs_time, vcs_dirty). Without this wiring the recurring
// stale-daemon-binary drift (three incidents to date) stays detectable only
// via DLQ-message heuristics instead of by comparing the running revision
// against repo HEAD.
func requireBuildIdentityWiring(t *testing.T, mainPath string) {
	t.Helper()
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read %s: %v", mainPath, err)
	}
	s := string(src)
	if !strings.Contains(s, "dashboard.InstallBuildIdentity()") {
		t.Errorf("%s must call dashboard.InstallBuildIdentity() at startup (read + publish build identity); no reference found", mainPath)
	}
	for _, key := range []string{`"vcs_revision"`, `"vcs_time"`, `"vcs_dirty"`} {
		if !strings.Contains(s, key) {
			t.Errorf("%s must log the build identity at startup with the %s field; no reference found", mainPath, key)
		}
	}
}

// TestDaemonLogsBuildIdentityAtStartup pins that THE DAEMON BINARY
// (cmd/bt-agent) embeds its build identity: startup wiring reads
// runtime/debug build info, logs revision/commit-time/dirty, and publishes
// the bt_build_info gauge so a running daemon's revision is comparable
// against repo HEAD.
func TestDaemonLogsBuildIdentityAtStartup(t *testing.T) {
	requireBuildIdentityWiring(t, "main.go")
}

// TestGardenerLogsBuildIdentityAtStartup pins the same build-identity wiring
// for the other long-running binary, cmd/bt-gardener (audited from here
// because the gardener package has no wiring test file of its own; the check
// is source-level, so cross-package distance does not matter).
func TestGardenerLogsBuildIdentityAtStartup(t *testing.T) {
	requireBuildIdentityWiring(t, "../bt-gardener/main.go")
}
