package agent

import (
	"reflect"
	"testing"
)

func TestDriftSiblingRestartRequiresTargetOwnership(t *testing.T) {
	oldHead, oldTree, oldBuild, oldSmoke, oldRestart := driftHeadFn, driftTreeFn, driftRebuildFn, driftSmokeTestFn, driftRestartFn
	t.Cleanup(func() {
		driftHeadFn, driftTreeFn, driftRebuildFn, driftSmokeTestFn, driftRestartFn = oldHead, oldTree, oldBuild, oldSmoke, oldRestart
	})
	driftHeadFn = func(string) (string, error) { return "newhead", nil }
	driftTreeFn = func(_, revision string) (string, error) { return revision, nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }
	driftSmokeTestFn = func(string) error { return nil }
	var direct []string
	driftRestartFn = func(unit string) error { direct = append(direct, unit); return nil }
	result, err := DriftWatchOnce(DriftWatchConfig{
		RepoDir: "fixture", RunningRevision: "oldhead", Binary: "bt-agent", AutoRebuild: true, AutoRestart: true, RestartSiblings: true,
		Targets: []RebuildTarget{{Name: "bt-agent", OutPath: "fixture-agent", Unit: "bt-agent"}, {Name: "bt-dashboard", OutPath: "fixture-dashboard", Unit: "bt-dashboard"}},
	})
	if err != nil || !result.Restarted || !reflect.DeepEqual(direct, []string{"bt-agent"}) {
		t.Fatalf("missing target ownership allowed sibling systemd bypass: direct=%v result=%+v err=%v", direct, result, err)
	}
}
