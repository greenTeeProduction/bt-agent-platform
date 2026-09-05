package domains

import "testing"

func TestArc42SeederTreeShape(t *testing.T) {
	tree := Arc42SeederTree()
	if tree.Type != "Sequence" {
		t.Fatalf("root must be a Sequence, got %s", tree.Type)
	}
	if tree.Name != "Arc42Seeder_Main" {
		t.Errorf("root name = %q, want Arc42Seeder_Main", tree.Name)
	}
	if len(tree.Children) != 3 {
		t.Fatalf("want 3 children, got %d", len(tree.Children))
	}

	tests := []struct {
		idx  int
		typ  string
		name string
	}{
		{0, "Condition", "TaskIsNotEmpty"},
		{1, "Action", "SeedProgramFromArc42Goals"},
		{2, "Action", "MarkSuccessful"},
	}
	for _, tc := range tests {
		child := tree.Children[tc.idx]
		if child.Type != tc.typ {
			t.Errorf("child %d type = %q, want %q", tc.idx, child.Type, tc.typ)
		}
		if child.Name != tc.name {
			t.Errorf("child %d name = %q, want %q", tc.idx, child.Name, tc.name)
		}
	}
}

func TestArc42SeederTreeRegistered(t *testing.T) {
	trees := AllDomainTrees()
	if _, ok := trees["arc42_seeder"]; !ok {
		t.Error("arc42_seeder missing from the domain tree map")
	}
	if _, ok := Descriptions["arc42_seeder"]; !ok {
		t.Error("arc42_seeder missing from Descriptions")
	}
}
