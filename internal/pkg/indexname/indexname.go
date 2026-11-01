// Package indexname derives the per-version Elasticsearch index/alias name
// component used consistently by the worker handlers (write), search usecase
// (read), and esindex repository (delete) — all three must derive the exact
// same string from a version name or indexes silently become unreachable.
package indexname

import (
	"fmt"
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

func sanitize(s string) string {
	out := invalidChars.ReplaceAllString(strings.ToLower(s), "_")
	return strings.TrimLeft(out, "-_+")
}

// An Elasticsearch wildcard ("*" only) cannot tell one version's indexes from
// another's: "{alias}-*" for version "v1" also matches version "v1-test"'s
// indexes, and "{alias}-v1a7-*" matches a version named "v1-v1a7-x". So
// before deleting the names a wildcard resolved to, check each with these.

// IsVersionIndex reports whether index is one of the concrete indexes of the
// version (ID versionID) behind alias "{prefix}-{type}-{versionSlug}": an
// assembly's "{alias}-v{versionID}a{n}-{unix}", or "{alias}-{unix}" (the
// orthology index, and the indexes from before indexes were per assembly).
func IsVersionIndex(index, alias string, versionID uint64) bool {
	rest, ok := strings.CutPrefix(index, alias+"-")
	if !ok {
		return false
	}
	return allDigits(rest) || isAssemblySuffix(rest, fmt.Sprintf("v%da", versionID))
}

// IsAssemblyIndex reports whether index is one of the concrete indexes of the
// assembly assemblyKey behind alias: "{alias}-{assemblyKey}-{unix}".
func IsAssemblyIndex(index, alias, assemblyKey string) bool {
	rest, ok := strings.CutPrefix(index, alias+"-"+assemblyKey+"-")
	return ok && allDigits(rest)
}

// isAssemblySuffix reports whether s is "{versionKey}{n}-{unix}", e.g.
// "v1a7-1700000000" for versionKey "v1a".
func isAssemblySuffix(s, versionKey string) bool {
	rest, ok := strings.CutPrefix(s, versionKey)
	if !ok {
		return false
	}
	assemblyID, unix, ok := strings.Cut(rest, "-")
	return ok && allDigits(assemblyID) && allDigits(unix)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
