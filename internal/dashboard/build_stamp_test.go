package dashboard

import (
	"runtime/debug"
	"testing"
)

// TestBuildIdentityStampedRevisionFallback proves the bare-repo drift fix: when
// the binary carries no vcs.revision (go build from a bare checkout), the
// ldflags-stamped revision is used so DriftStatus is no longer inert.
func TestBuildIdentityStampedRevisionFallback(t *testing.T) {
	orig := stampedRevision
	t.Cleanup(func() { stampedRevision = orig })

	// No build info at all + a stamp -> stamp is used.
	stampedRevision = "abc123def456"
	if got := BuildIdentityFromBuildInfo(nil).Revision; got != "abc123def456" {
		t.Fatalf("nil buildinfo with stamp: Revision = %q, want abc123def456", got)
	}

	// Real vcs.revision present -> it wins over the stamp.
	stampedRevision = "abc123def456"
	bi := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "realvcsrevision"}}}
	if got := BuildIdentityFromBuildInfo(bi).Revision; got != "realvcsrevision" {
		t.Fatalf("vcs.revision must win over stamp: Revision = %q, want realvcsrevision", got)
	}

	// No stamp and no build info -> unchanged sentinel (drift stays inert, as before).
	stampedRevision = ""
	if got := BuildIdentityFromBuildInfo(nil).Revision; got != unknownBuildValue {
		t.Fatalf("no stamp, no buildinfo: Revision = %q, want %q", got, unknownBuildValue)
	}
}
