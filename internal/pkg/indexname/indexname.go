// Package indexname derives the per-version Elasticsearch index/alias name
// component used consistently by the worker handlers (write), search usecase
// (read), and esindex repository (delete) — all three must derive the exact
// same string from a version name or indexes silently become unreachable.
package indexname

import (
	"regexp"
	"strings"
)

// Characters Elasticsearch forbids anywhere in an index name:
// \, /, *, ?, ", <, >, |, ` ` (space), ,, #
var invalidChars = regexp.MustCompile(`[\\/*?"<>|,# ]+`)

// FromVersionName converts a version name into a value safe to embed as a
// component of an Elasticsearch index/alias name: lowercased, with any
// ES-forbidden character collapsed to a single underscore, and leading
// -/_/+ trimmed since ES also rejects those as the first character.
func FromVersionName(name string) string {
	return sanitize(name)
}

// FromSpecies converts an Assembly Version's species code (e.g. "Hsap") into
// a value safe to embed as a component of an Elasticsearch index name — same
// rules as FromVersionName (ES index names must be all-lowercase, among
// other restrictions), since species codes are admin-provided free text just
// like version names.
func FromSpecies(species string) string {
	return sanitize(species)
}

func sanitize(s string) string {
	out := invalidChars.ReplaceAllString(strings.ToLower(s), "_")
	return strings.TrimLeft(out, "-_+")
}
