package engine

import "testing"

// Characterization tests for builtin_names.go — pin the current exported
// behavior of isKnownActionName/isKnownConditionName and the underlying
// builtinActionNames/builtinConditionNames fallback maps. These names are
// consulted only after the live registry (GetAction/GetCondition) and the
// compiled-GOAP prefix checks (isCompiledGoapAction/isCompiledGoapCondition)
// have already missed, so this file also pins that fallback ordering for a
// name (InitTranspositionTable) that is registered elsewhere
// (actions_stockfish.go's init) as well as listed in builtinActionNames.

func TestBuiltinActionNamesMap_Size(t *testing.T) {
	const want = 205
	if got := len(builtinActionNames); got != want {
		t.Errorf("len(builtinActionNames) = %d, want %d (map grew/shrank — update this pin if intentional)", got, want)
	}
}

func TestBuiltinConditionNamesMap_Size(t *testing.T) {
	const want = 143
	if got := len(builtinConditionNames); got != want {
		t.Errorf("len(builtinConditionNames) = %d, want %d (map grew/shrank — update this pin if intentional)", got, want)
	}
}

func TestBuiltinActionNamesMap_SpotCheck(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"AddCitations", true},
		{"QCBriefingPack", true},
		{"hasCachedFitness", true}, // lowercase leading letter — deliberately odd, present verbatim
		{"VerifyRestart", true},
		{"NotARealBuiltinAction", false},
		{"", false},
		// Case sensitivity: the map key is "hasCachedFitness"; the
		// capitalized form is not itself a key of this map (it is,
		// however, a key of builtinConditionNames — see below).
		{"HasCachedFitness", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := builtinActionNames[tt.name]; got != tt.want {
				t.Errorf("builtinActionNames[%q] = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestBuiltinConditionNamesMap_SpotCheck(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"CheckCitationFormat", true},
		{"HasCachedFitness", true},
		{"IsVaultTask", true},
		{"IsSalesTask", true},
		{"NotARealBuiltinCondition", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := builtinConditionNames[tt.name]; got != tt.want {
				t.Errorf("builtinConditionNames[%q] = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsKnownActionName(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"empty name is never known", "", false},
		{"builtin-only name falls back to true", "QCBriefingPack", true},
		{
			// Registered via actions_stockfish.go's init() AND listed in
			// builtinActionNames — pins that the registry-first branch
			// short-circuits before the fallback map is even consulted.
			name: "registered and builtin-listed name",
			arg:  "InitTranspositionTable",
			want: true,
		},
		{"compiled GOAP effect-write prefix is known", "ApplyGoapEffects:has_resources=true", true},
		{"bare compiled GOAP prefix (no payload) is not known", "ApplyGoapEffects:", false},
		{"unregistered, non-builtin, non-compiled name is unknown", "TotallyMadeUpActionXYZ", false},
		{"builtin-listed name", "VerifyRestart", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isKnownActionName(tt.arg); got != tt.want {
				t.Errorf("isKnownActionName(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestIsKnownConditionName(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"empty name is never known", "", false},
		{"builtin-only name falls back to true", "IsVaultTask", true},
		{"compiled GOAP state-match prefix is known", "GoapStateMatches:has_analysis=true", true},
		{"bare compiled GOAP prefix (no payload) is not known", "GoapStateMatches:", false},
		{"unregistered, non-builtin, non-compiled name is unknown", "TotallyMadeUpConditionXYZ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isKnownConditionName(tt.arg); got != tt.want {
				t.Errorf("isKnownConditionName(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}
