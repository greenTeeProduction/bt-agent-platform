package agent

import (
	"fmt"
	"sync"
	"testing"
)

func TestReviewRegistrySnapshots(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	def := Definition{Name: "snapshot", Inputs: []InputSpec{{Name: "original"}}, Metadata: map[string]string{"key": "original"}, Quality: &QualitySpec{RequiredKeywords: []string{"original"}}}
	created, err := r.Create(def)
	if err != nil {
		t.Fatal(err)
	}
	def.Metadata["key"] = "input changed"
	created.Definition.Inputs[0].Name = "changed"
	for _, inst := range r.List() {
		inst.Definition.Quality.RequiredKeywords[0] = "changed"
	}
	got, err := r.Get(def.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Definition.Inputs[0].Name != "original" || got.Definition.Metadata["key"] != "original" || got.Definition.Quality.RequiredKeywords[0] != "original" {
		t.Fatalf("mutable snapshot escaped: %+v", got.Definition)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			if err := r.UpdateSchedule(def.Name, fmt.Sprint(i)); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Go(func() {
		for range 1000 {
			for _, inst := range r.List() {
				_ = fmt.Sprint(inst.Definition.Schedule)
			}
		}
	})
	wg.Wait()
}
