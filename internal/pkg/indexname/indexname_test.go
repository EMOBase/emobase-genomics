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

// A gene lookup reports each document's assembly from the concrete index it
// came from, so only the exact name the worker handlers build may yield a key:
// anything else would attribute a gene to a wrong or non-existent assembly.
func TestAssemblyKey_OnlyFromHandlerBuiltNames(t *testing.T) {
	const alias = "p-synonym-v1"
	for index, want := range map[string]string{
		"p-synonym-v1-v1a7-1700000000":         "v1a7",
		"p-synonym-v1-v1a12-1700000000":        "v1a12",
		"p-synonym-v1-1700000000":              "", // pre-assembly index: no assembly
		"p-synonym-v1-v1a7-1700000000-reindex": "",
		"p-synonym-v1-v1a7":                    "", // no timestamp
		"p-synonym-v1-v1a07-1700000000":        "", // not FormatAssemblyID's form
		"p-synonym-v1-va7-1700000000":          "",
		"p-synonym-v11-v11a7-1700000000":       "", // another alias
	} {
		got, ok := AssemblyKey(index, alias)
		if got != want || ok != (want != "") {
			t.Errorf("AssemblyKey(%q) = %q, %v, want %q, %v", index, got, ok, want, want != "")
		}
	}
}
