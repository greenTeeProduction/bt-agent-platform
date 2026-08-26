package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/goap"
)

func TestAllSections(t *testing.T) {
	got := allSections()
	want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("allSections() = %v, want %v", got, want)
	}
}

func TestParseSections(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []int
	}{
		{"single value", "5", []int{5}},
		{"multiple values sorted", "5,1,9", []int{1, 5, 9}},
		{"whitespace trimmed", " 2 , 3 ", []int{2, 3}},
		{"out of range values dropped", "0,13,7", []int{7}},
		{"non-numeric values dropped", "abc,4", []int{4}},
		{"boundary values kept", "1,12", []int{1, 12}},
		{"duplicates preserved", "3,3,1", []int{1, 3, 3}},
		{"empty string yields empty slice", "", []int{}},
		{"all invalid yields empty slice", "0,13,abc", []int{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSections(tc.input)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseSections(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	cases := []struct {
		name  string
		slice []int
		n     int
		want  bool
	}{
		{"present", []int{1, 2, 3}, 2, true},
		{"absent", []int{1, 2, 3}, 4, false},
		{"empty slice", nil, 1, false},
		{"present at first index", []int{5, 6}, 5, true},
		{"present at last index", []int{5, 6}, 6, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contains(tc.slice, tc.n); got != tc.want {
				t.Errorf("contains(%v, %d) = %v, want %v", tc.slice, tc.n, got, tc.want)
			}
		})
	}
}

func TestBuildSectionMap(t *testing.T) {
	m := buildSectionMap()
	if len(m) != len(goap.SectionMappings) {
		t.Fatalf("buildSectionMap() len = %d, want %d", len(m), len(goap.SectionMappings))
	}
	for _, sm := range goap.SectionMappings {
		got, ok := m[sm.ActionName]
		if !ok {
			t.Errorf("buildSectionMap() missing entry for action %q", sm.ActionName)
			continue
		}
		if !reflect.DeepEqual(got, sm) {
			t.Errorf("buildSectionMap()[%q] = %+v, want %+v", sm.ActionName, got, sm)
		}
	}
}

func TestSetChainState(t *testing.T) {
	t.Run("initializes nil map", func(t *testing.T) {
		bb := &engine.Blackboard{}
		setChainState(bb, "foo", "bar")
		if bb.ChainState == nil {
			t.Fatal("expected ChainState to be initialized")
		}
		if bb.ChainState["foo"] != "bar" {
			t.Errorf("ChainState[%q] = %v, want %q", "foo", bb.ChainState["foo"], "bar")
		}
	})

	t.Run("sets into existing map without clobbering other keys", func(t *testing.T) {
		bb := &engine.Blackboard{ChainState: map[string]any{"existing": 1}}
		setChainState(bb, "foo", 42)
		if bb.ChainState["existing"] != 1 {
			t.Errorf("existing key clobbered: %v", bb.ChainState["existing"])
		}
		if bb.ChainState["foo"] != 42 {
			t.Errorf("ChainState[%q] = %v, want 42", "foo", bb.ChainState["foo"])
		}
	})

	t.Run("overwrites an existing key", func(t *testing.T) {
		bb := &engine.Blackboard{ChainState: map[string]any{"foo": "old"}}
		setChainState(bb, "foo", "new")
		if bb.ChainState["foo"] != "new" {
			t.Errorf("ChainState[%q] = %v, want %q", "foo", bb.ChainState["foo"], "new")
		}
	})
}

func TestSetSectionDoneAndIsSectionDone(t *testing.T) {
	for section := 1; section <= 12; section++ {

		t.Run(fmt.Sprintf("section %d", section), func(t *testing.T) {
			var ws goap.DocPlannerWorldState
			if isSectionDone(ws, section) {
				t.Fatalf("isSectionDone(section %d) = true before setSectionDone", section)
			}
			setSectionDone(&ws, section)
			if !isSectionDone(ws, section) {
				t.Fatalf("isSectionDone(section %d) = false after setSectionDone", section)
			}
			// No other section's flag should have flipped.
			for other := 1; other <= 12; other++ {
				if other == section {
					continue
				}
				if isSectionDone(ws, other) {
					t.Errorf("setSectionDone(%d) unexpectedly marked section %d done", section, other)
				}
			}
		})
	}

	t.Run("out of range section is a no-op for setSectionDone", func(t *testing.T) {
		var ws goap.DocPlannerWorldState
		setSectionDone(&ws, 0)
		setSectionDone(&ws, 13)
		if ws.AllDone() {
			t.Fatalf("expected no sections marked done, got %+v", ws)
		}
	})

	t.Run("out of range section returns false for isSectionDone", func(t *testing.T) {
		ws := goap.DocPlannerWorldState{
			Section1Done: true, Section2Done: true, Section3Done: true, Section4Done: true,
			Section5Done: true, Section6Done: true, Section7Done: true, Section8Done: true,
			Section9Done: true, Section10Done: true, Section11Done: true, Section12Done: true,
		}
		if isSectionDone(ws, 0) {
			t.Error("isSectionDone(0) = true, want false")
		}
		if isSectionDone(ws, 13) {
			t.Error("isSectionDone(13) = true, want false")
		}
	})
}

func TestFileHash(t *testing.T) {
	t.Run("missing file returns empty string", func(t *testing.T) {
		if got := fileHash(filepath.Join(t.TempDir(), "does-not-exist")); got != "" {
			t.Errorf("fileHash(missing) = %q, want empty string", got)
		}
	})

	t.Run("existing file returns sha256 hex digest", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.txt")
		content := []byte("characterization test content")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		want := fmt.Sprintf("%x", sha256.Sum256(content))
		if got := fileHash(path); got != want {
			t.Errorf("fileHash(%q) = %q, want %q", path, got, want)
		}
	})

	t.Run("deterministic across calls", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.txt")
		if err := os.WriteFile(path, []byte("same content"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		first := fileHash(path)
		second := fileHash(path)
		if first != second {
			t.Errorf("fileHash not deterministic: %q vs %q", first, second)
		}
	})
}

func TestHashSectionSources(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "present.txt")
	missing := filepath.Join(dir, "absent.txt")
	if err := os.WriteFile(existing, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	const testSection = 999
	origFiles, hadEntry := sectionSourceFiles[testSection]
	t.Cleanup(func() {
		if hadEntry {
			sectionSourceFiles[testSection] = origFiles
		} else {
			delete(sectionSourceFiles, testSection)
		}
	})
	sectionSourceFiles[testSection] = []string{existing, missing}

	got := hashSectionSources(testSection, nil)

	h := sha256.New()
	h.Write([]byte(existing + ":"))
	h.Write([]byte("hello world"))
	h.Write([]byte(missing + ":missing"))
	want := fmt.Sprintf("%x", h.Sum(nil))

	if got != want {
		t.Errorf("hashSectionSources(%d) = %q, want %q", testSection, got, want)
	}

	t.Run("deterministic across calls", func(t *testing.T) {
		if again := hashSectionSources(testSection, nil); again != got {
			t.Errorf("hashSectionSources not deterministic: %q vs %q", again, got)
		}
	})

	t.Run("changes when a source file's content changes", func(t *testing.T) {
		if err := os.WriteFile(existing, []byte("different content"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if changed := hashSectionSources(testSection, nil); changed == got {
			t.Errorf("hashSectionSources did not change after source content changed")
		}
	})
}

func TestHashSectionSources_UnknownSection(t *testing.T) {
	got := hashSectionSources(-1, nil)
	want := "section--1-no-sources"
	if got != want {
		t.Errorf("hashSectionSources(-1) = %q, want %q", got, want)
	}
}

func TestDocgenState_IsStale(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("source content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	const testSection = 998
	origFiles, hadEntry := sectionSourceFiles[testSection]
	t.Cleanup(func() {
		if hadEntry {
			sectionSourceFiles[testSection] = origFiles
		} else {
			delete(sectionSourceFiles, testSection)
		}
	})
	sectionSourceFiles[testSection] = []string{src}
	currentHash := hashSectionSources(testSection, nil)
	key := fmt.Sprintf("%d", testSection)

	cases := []struct {
		name  string
		state docgenState
		want  bool
	}{
		{
			name:  "nil SourceHash is stale",
			state: docgenState{},
			want:  true,
		},
		{
			name:  "nil SectionHash is stale",
			state: docgenState{SourceHash: map[string]string{key: currentHash}},
			want:  true,
		},
		{
			name:  "missing key for section is stale",
			state: docgenState{SourceHash: map[string]string{}, SectionHash: map[string]string{}},
			want:  true,
		},
		{
			name:  "matching recorded hash is not stale",
			state: docgenState{SourceHash: map[string]string{key: currentHash}, SectionHash: map[string]string{key: "irrelevant"}},
			want:  false,
		},
		{
			name:  "differing recorded hash is stale",
			state: docgenState{SourceHash: map[string]string{key: "deadbeef"}, SectionHash: map[string]string{key: "irrelevant"}},
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.isStale(testSection); got != tc.want {
				t.Errorf("isStale(%d) = %v, want %v", testSection, got, tc.want)
			}
		})
	}
}

func TestDocgenState_IsGraphStale(t *testing.T) {
	writeGraphReport := func(t *testing.T, content []byte) {
		t.Helper()
		dir := t.TempDir()
		t.Chdir(dir)
		if err := os.MkdirAll("graphify-out", 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join("graphify-out", "GRAPH_REPORT.md"), content, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	t.Run("matching hash is not stale", func(t *testing.T) {
		content := []byte("graph report v1")
		writeGraphReport(t, content)
		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		ds := docgenState{GraphHash: hash}
		if ds.isGraphStale() {
			t.Error("isGraphStale() = true, want false")
		}
	})

	t.Run("differing hash is stale", func(t *testing.T) {
		writeGraphReport(t, []byte("graph report v2"))
		ds := docgenState{GraphHash: "deadbeef"}
		if !ds.isGraphStale() {
			t.Error("isGraphStale() = false, want true")
		}
	})

	t.Run("missing file with empty recorded hash is not stale", func(t *testing.T) {
		t.Chdir(t.TempDir())
		ds := docgenState{GraphHash: ""}
		if ds.isGraphStale() {
			t.Error("isGraphStale() = true, want false")
		}
	})

	t.Run("missing file with non-empty recorded hash is stale", func(t *testing.T) {
		t.Chdir(t.TempDir())
		ds := docgenState{GraphHash: "some-previous-hash"}
		if !ds.isGraphStale() {
			t.Error("isGraphStale() = false, want true")
		}
	})
}

func TestDocgenState_JSONRoundTrip(t *testing.T) {
	ds := docgenState{
		LastRun:     "2026-07-18T00:00:00Z",
		GraphHash:   "abc123",
		SourceHash:  map[string]string{"1": "aaa", "2": "bbb"},
		SectionHash: map[string]string{"1": "ccc"},
	}
	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	var restored docgenState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(ds, restored) {
		t.Errorf("JSON round trip mismatch: got %+v, want %+v", restored, ds)
	}
}

// TestSectionReady pins the intended dependency-gating behavior for a
// section: it must be considered ready only when every section it
// DependsOn has already been marked done.
func TestSectionReady(t *testing.T) {
	cases := []struct {
		name string
		sm   goap.SectionMapping
		ws   goap.DocPlannerWorldState
		want bool
	}{
		{
			name: "no dependencies is always ready",
			sm:   goap.SectionMapping{Number: 1, DependsOn: nil},
			ws:   goap.DocPlannerWorldState{},
			want: true,
		},
		{
			name: "single dependency satisfied",
			sm:   goap.SectionMapping{Number: 4, DependsOn: []int{1}},
			ws:   goap.DocPlannerWorldState{Section1Done: true},
			want: true,
		},
		{
			name: "single dependency unsatisfied",
			sm:   goap.SectionMapping{Number: 4, DependsOn: []int{1}},
			ws:   goap.DocPlannerWorldState{},
			want: false,
		},
		{
			name: "multiple dependencies, one unsatisfied",
			sm:   goap.SectionMapping{Number: 5, DependsOn: []int{1, 4}},
			ws:   goap.DocPlannerWorldState{Section1Done: true},
			want: false,
		},
		{
			name: "multiple dependencies, all satisfied",
			sm:   goap.SectionMapping{Number: 5, DependsOn: []int{1, 4}},
			ws:   goap.DocPlannerWorldState{Section1Done: true, Section4Done: true},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sectionReady(tc.ws, tc.sm); got != tc.want {
				t.Errorf("sectionReady() = %v, want %v", got, tc.want)
			}
		})
	}
}
