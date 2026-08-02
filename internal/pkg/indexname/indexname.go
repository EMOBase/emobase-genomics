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
	s := invalidChars.ReplaceAllString(strings.ToLower(name), "_")
	return strings.TrimLeft(s, "-_+")
}
