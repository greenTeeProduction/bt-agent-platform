package engine

import (
	"sort"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestRegisteredNames_SortedAndComplete(t *testing.T) {
	RegisterAction("test_registered_names_probe_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	RegisterCondition("test_registered_names_probe_condition", func(*Blackboard) bool { return true })

	actions := RegisteredActionNames()
	if !sort.StringsAreSorted(actions) {
		t.Fatal("action names must be sorted")
	}
	conditions := RegisteredConditionNames()
	if !sort.StringsAreSorted(conditions) {
		t.Fatal("condition names must be sorted")
	}
	contains := func(names []string, want string) bool {
		i := sort.SearchStrings(names, want)
		return i < len(names) && names[i] == want
	}
	if !contains(actions, "test_registered_names_probe_action") {
		t.Fatal("registered action missing from listing")
	}
	if !contains(conditions, "test_registered_names_probe_condition") {
		t.Fatal("registered condition missing from listing")
	}
}
