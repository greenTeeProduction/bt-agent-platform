package engine

import (
	"slices"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestRegisteredNames_SortedAndComplete(t *testing.T) {
	RegisterAction("test_registered_names_probe_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	RegisterCondition("test_registered_names_probe_condition", func(*Blackboard) bool { return true })

	actions := RegisteredActionNames()
	if !slices.IsSorted(actions) {
		t.Fatal("action names must be sorted")
	}
	conditions := RegisteredConditionNames()
	if !slices.IsSorted(conditions) {
		t.Fatal("condition names must be sorted")
	}
	contains := func(names []string, want string) bool {
		_, ok := slices.BinarySearch(names, want)
		return ok
	}
	if !contains(actions, "test_registered_names_probe_action") {
		t.Fatal("registered action missing from listing")
	}
	if !contains(conditions, "test_registered_names_probe_condition") {
		t.Fatal("registered condition missing from listing")
	}
}
