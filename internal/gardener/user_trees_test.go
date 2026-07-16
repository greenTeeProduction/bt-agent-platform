package gardener

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Personal-tree gardener support (ADR-133 Phase 5).

func writeUserTree(t *testing.T, usersRoot, user, treeID string) string {
	t.Helper()
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: treeID,
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "GeneratePlan"},
			{Type: "Action", Name: "ExecLLMCall"},
		},
	}
	path, err := evolution.SaveNamedTree(filepath.Join(usersRoot, user, "trees"), treeID, tree)
	if err != nil {
		t.Fatalf("save user tree: %v", err)
	}
	return path
}

func TestRegistryWithUsers_LoadsPersonalTrees(t *testing.T) {
	storageDir := t.TempDir()
	usersRoot := t.TempDir()
	path := writeUserTree(t, usersRoot, "nico", "goal:automate_reports")

	r := NewRegistryWithUsers(storageDir, usersRoot)

	var entry *TreeEntry
	for _, e := range r.List() {
		if e.User == "nico" {
			ec := e
			entry = &ec
			break
		}
	}
	if entry == nil {
		t.Fatal("personal tree not loaded from user workspace")
	}
	// Entry name must be the tree ID (root node name) so reflection filtering
	// and dynamic resolution agree on the tree's identity.
	if entry.Name != "goal:automate_reports" {
		t.Errorf("entry name = %q, want root node name %q", entry.Name, "goal:automate_reports")
	}
	if entry.FilePath != path {
		t.Errorf("entry file path = %q, want %q (SaveTree must write back into the user workspace)", entry.FilePath, path)
	}
	if !entry.Active {
		t.Error("personal tree should be active")
	}

	// SaveTree round-trips into the user's own workspace.
	if err := r.SaveTree(*entry); err != nil {
		t.Fatalf("SaveTree: %v", err)
	}
	reloaded := NewRegistryWithUsers(storageDir, usersRoot)
	found := false
	for _, e := range reloaded.List() {
		if e.User == "nico" && e.Name == "goal:automate_reports" {
			found = true
		}
	}
	if !found {
		t.Error("personal tree lost after SaveTree + reload")
	}
}

func TestRegistryWithUsers_CollidingTreeIDsStayDistinct(t *testing.T) {
	storageDir := t.TempDir()
	usersRoot := t.TempDir()
	writeUserTree(t, usersRoot, "alice", "goal:automate_reports")
	writeUserTree(t, usersRoot, "bob", "goal:automate_reports")

	r := NewRegistryWithUsers(storageDir, usersRoot)

	names := make(map[string]int)
	users := 0
	for _, e := range r.List() {
		if e.User != "" {
			users++
			names[e.Name]++
		}
	}
	if users != 2 {
		t.Fatalf("loaded %d personal trees, want 2", users)
	}
	for name, n := range names {
		if n > 1 {
			t.Errorf("registry entry name %q used %d times — collisions must be disambiguated", name, n)
		}
	}
}

func TestRecordsForEntry_PersonalTreesUseStrictFiltering(t *testing.T) {
	records := make([]evolution.Record, 0, 3)
	records = append(records,
		evolution.Record{TaskID: "1", TreeName: "default", Outcome: evolution.Success},
		evolution.Record{TaskID: "2", TreeName: "", Outcome: evolution.Success},
	)

	// Shared entry keeps the backward-compat fallback (no match → all).
	shared := recordsForEntry(records, TreeEntry{Name: "goal:x"})
	if len(shared) != 2 {
		t.Errorf("shared entry records = %d, want fallback to all 2", len(shared))
	}

	// Personal entry must see only its own evidence — here none.
	personal := recordsForEntry(records, TreeEntry{Name: "goal:x", User: "nico"})
	if len(personal) != 0 {
		t.Errorf("personal entry records = %d, want 0 (strict)", len(personal))
	}

	records = append(records, evolution.Record{TaskID: "3", TreeName: "goal:x", Outcome: evolution.Success})
	personal = recordsForEntry(records, TreeEntry{Name: "goal:x", User: "nico"})
	if len(personal) != 1 {
		t.Errorf("personal entry records = %d, want exactly its own 1", len(personal))
	}
}

func TestEvolveTreeV2_UserTreeWithoutEvidenceIsSkipped(t *testing.T) {
	refStore, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("ref store: %v", err)
	}
	mt, err := NewMetricsTracker(t.TempDir())
	if err != nil {
		t.Fatalf("metrics tracker: %v", err)
	}
	g := NewGardener(Config{
		Registry:       NewRegistry(t.TempDir()),
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   2,
	})

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "goal:automate_reports",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "GeneratePlan"},
			{Type: "Action", Name: "ExecLLMCall"},
		},
	}
	entry := TreeEntry{Name: "goal:automate_reports", Tree: tree, User: "nico", Active: true}

	metrics := g.evolveTreeV2(entry, DefaultEvolveV2Config())
	if !metrics.SkippedNoEvidence {
		t.Error("personal tree without reflections must be skipped by the evidence gate")
	}
	if metrics.Mutations != 0 {
		t.Errorf("mutations = %d, want 0 for evidence-gated tree", metrics.Mutations)
	}

	// A seed reflection (what compile-time seeding writes) unfreezes it.
	if err := refStore.Save(&evolution.Record{
		TaskID: "seed-goal_automate_reports", TreeName: "goal:automate_reports",
		Task: "compile-time validation", Outcome: evolution.Success,
	}); err != nil {
		t.Fatalf("save seed reflection: %v", err)
	}
	metrics = g.evolveTreeV2(entry, DefaultEvolveV2Config())
	if metrics.SkippedNoEvidence {
		t.Error("seed reflection should satisfy the evidence gate")
	}
}

func TestBankFor_PersonalTreesGetPerUserBank(t *testing.T) {
	sharedBank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("shared bank: %v", err)
	}
	usersRoot := t.TempDir()
	g := NewGardener(Config{
		ExperienceBank:     sharedBank,
		UserExperienceRoot: usersRoot,
	})

	shared := g.bankFor(TreeEntry{Name: "default"})
	if shared != sharedBank {
		t.Error("shared tree must use the shared bank")
	}

	userBank := g.bankFor(TreeEntry{Name: "goal:x", User: "nico"})
	if userBank == nil || userBank == sharedBank {
		t.Fatal("personal tree must get its own per-user bank")
	}
	if again := g.bankFor(TreeEntry{Name: "goal:y", User: "nico"}); again != userBank {
		t.Error("per-user bank must be cached and reused across the user's trees")
	}
	if other := g.bankFor(TreeEntry{Name: "goal:x", User: "alice"}); other == userBank {
		t.Error("different users must not share a personal bank")
	}
}

// TestRecordsForEntry_CollidingUsersFilterByRecordUser reproduces the
// cross-user evidence bug: two users own trees with the same real tree ID
// ("goal:x"). The registry disambiguates the second-loaded entry's display
// Name (e.g. "bob_goal:x") but the underlying Tree.Name — the real tree ID —
// is untouched. Reflection scoring must (1) match renamed entries on their
// real tree ID, not the disambiguated display name, and (2) filter matched
// records down to the owning user so a plain-named entry never scores on
// another user's records.
func TestRecordsForEntry_CollidingUsersFilterByRecordUser(t *testing.T) {
	records := []evolution.Record{
		{TaskID: "a1", TreeName: "goal:x", User: "alice", Outcome: evolution.Success},
		{TaskID: "b1", TreeName: "goal:x", User: "bob", Outcome: evolution.Success},
	}

	// Plain-named entry (alice's, loaded first — kept the bare tree ID).
	aliceEntry := TreeEntry{
		Name: "goal:x", User: "alice",
		Tree: &evolution.SerializableNode{Name: "goal:x"},
	}
	aliceRecords := recordsForEntry(records, aliceEntry)
	if len(aliceRecords) != 1 || aliceRecords[0].TaskID != "a1" {
		t.Errorf("alice's plain-named entry records = %+v, want only her own record a1 (not bob's)", aliceRecords)
	}

	// Renamed entry (bob's, loaded second after a collision) — display Name
	// carries the "bob_" prefix but Tree.Name still holds the real tree ID.
	bobEntry := TreeEntry{
		Name: "bob_goal:x", User: "bob",
		Tree: &evolution.SerializableNode{Name: "goal:x"},
	}
	bobRecords := recordsForEntry(records, bobEntry)
	if len(bobRecords) != 1 || bobRecords[0].TaskID != "b1" {
		t.Errorf("bob's renamed entry records = %+v, want only his own record b1 matched via the real tree ID", bobRecords)
	}
}

func TestBankFor_TransientOpenErrorDoesNotPermanentlyCacheSharedBank(t *testing.T) {
	sharedBank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("shared bank: %v", err)
	}
	usersRoot := t.TempDir()
	userDir := filepath.Join(usersRoot, "nico")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}
	// Block the per-user experience directory with a regular file so
	// NewExperienceBank's os.MkdirAll fails — a transient, recoverable error.
	blockedPath := filepath.Join(userDir, "experience")
	if err := os.WriteFile(blockedPath, []byte("block"), 0644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	g := NewGardener(Config{ExperienceBank: sharedBank, UserExperienceRoot: usersRoot})

	got := g.bankFor(TreeEntry{Name: "goal:x", User: "nico"})
	if got != sharedBank {
		t.Fatalf("expected fallback to shared bank while the per-user path is blocked")
	}

	// Clear the transient failure — the user's own bank can now open.
	if err := os.Remove(blockedPath); err != nil {
		t.Fatalf("remove blocking file: %v", err)
	}

	got2 := g.bankFor(TreeEntry{Name: "goal:x", User: "nico"})
	if got2 == sharedBank {
		t.Error("bankFor must not permanently cache the shared bank after a transient open error; it should retry opening the user's own bank on the next call")
	}
}

func TestBankFor_NoUserRootFallsBackToShared(t *testing.T) {
	sharedBank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("shared bank: %v", err)
	}
	g := NewGardener(Config{ExperienceBank: sharedBank})
	if got := g.bankFor(TreeEntry{Name: "goal:x", User: "nico"}); got != sharedBank {
		t.Error("without UserExperienceRoot personal trees must fall back to the shared bank")
	}
}
