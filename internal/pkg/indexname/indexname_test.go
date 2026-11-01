package indexname

import "testing"

// Deleting a version or an assembly resolves a wildcard, which also matches
// indexes of a version whose name merely starts with this one's (version names
// may contain "-"). Only this version's or assembly's own indexes may pass;
// the rest belong to another version and must survive.
func TestIsVersionIndexAndIsAssemblyIndex_OnlyOwnIndexes(t *testing.T) {
	const alias = "p-synonym-v1" // version "v1", ID 1
	for index, want := range map[string]bool{
		"p-synonym-v1-v1a7-1700000000":       true,  // assembly 7 of version 1
		"p-synonym-v1-v1a12-1700000000":      true,  // assembly 12 of version 1
		"p-synonym-v1-1700000000":            true,  // orthology shape / pre-assembly index
		"p-synonym-v1-test-v2a1-1700000000":  false, // version "v1-test"
		"p-synonym-v1-v1abc-v5a2-1700000000": false, // version "v1-v1abc"
		"p-synonym-v1-2-1700000000":          false, // version "v1-2"
		"p-synonym-v1-v12a7-1700000000":      false, // version ID 12, not 1
		"p-synonym-v11-v11a7-1700000000":     false, // another alias
	} {
		if got := IsVersionIndex(index, alias, 1); got != want {
			t.Errorf("IsVersionIndex(%q) = %v, want %v", index, got, want)
		}
	}

	for index, want := range map[string]bool{
		"p-synonym-v1-v1a7-1700000000":         true,
		"p-synonym-v1-v1a7-x-v9a1-1700000000":  false, // version "v1-v1a7-x"
		"p-synonym-v1-v1a70-1700000000":        false, // assembly 70
		"p-synonym-v1-v1a7-1700000000-reindex": false,
	} {
		if got := IsAssemblyIndex(index, alias, "v1a7"); got != want {
			t.Errorf("IsAssemblyIndex(%q) = %v, want %v", index, got, want)
		}
	}
}
