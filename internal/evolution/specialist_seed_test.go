package evolution

import (
	"maps"
	"slices"
	"testing"
)

// SeedSpecialistRegistry gives production populations resurrection material:
// without it, Population.Specialists stays nil everywhere outside tests and
// the crisis-resurrection half of f5f47894 is dead code.
func TestSeedSpecialistRegistry_PreloadsValidatedArchetypes(t *testing.T) {
	reg := SeedSpecialistRegistry()
	if reg == nil || len(reg.Archetypes) == 0 {
		t.Fatal("SeedSpecialistRegistry must pre-load the expert specialist archetypes")
	}
	if _, ok := reg.Archetypes["goap"]; !ok {
		t.Fatalf("expected the goap specialist archetype, got %v", func() []string {
			keys := slices.Collect(maps.Keys(reg.Archetypes))
			return keys
		}())
	}
}
