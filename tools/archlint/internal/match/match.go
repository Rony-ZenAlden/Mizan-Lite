// Package match provides doublestar-style glob matching for package paths and
// file paths, plus a helper to recognise standard-library import paths.
//
//   - matches within a single path segment
//     ?   matches a single non-slash character
//     **  matches across segments (including none)
//
// Trailing "/**" also matches the base itself: "a/**" matches "a", "a/b", "a/b/c".
package match

import (
	"regexp"
	"strings"
	"sync"
)

var cache sync.Map // pattern string -> *regexp.Regexp

func compile(pattern string) *regexp.Regexp {
	if v, ok := cache.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	var b strings.Builder
	b.WriteByte('^')
	n := len(pattern)
	for i := 0; i < n; {
		rest := pattern[i:]
		switch {
		case strings.HasPrefix(rest, "/**/"): // a/**/b → a/(any dirs/)?b
			b.WriteString("/(?:.*/)?")
			i += 4
		case rest == "/**": // trailing a/** → matches a and a/anything
			b.WriteString("(?:/.*)?")
			i += 3
		case i == 0 && strings.HasPrefix(rest, "**/"): // **/b → (any dirs/)?b
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(rest, "**"): // ** anywhere else → any chars
			b.WriteString(".*")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			// Literal run up to the next wildcard, stopping before "/**" so the
			// slash keeps its structural meaning.
			j := i + 1
			for j < n && !isWildcardStart(pattern, j) {
				j++
			}
			b.WriteString(regexp.QuoteMeta(pattern[i:j]))
			i = j
		}
	}
	b.WriteByte('$')
	re := regexp.MustCompile(b.String())
	cache.Store(pattern, re)
	return re
}

func isWildcardStart(pattern string, j int) bool {
	if pattern[j] == '*' || pattern[j] == '?' {
		return true
	}
	return strings.HasPrefix(pattern[j:], "/**")
}

// Glob reports whether s matches the glob pattern.
func Glob(pattern, s string) bool { return compile(pattern).MatchString(s) }

// Any reports whether s matches any of the patterns.
func Any(patterns []string, s string) bool {
	for _, p := range patterns {
		if Glob(p, s) {
			return true
		}
	}
	return false
}

// IsStd reports whether an import path refers to the Go standard library.
// Standard-library import paths have no dot in their first segment.
func IsStd(importPath string) bool {
	seg := importPath
	if i := strings.IndexByte(importPath, '/'); i >= 0 {
		seg = importPath[:i]
	}
	return !strings.Contains(seg, ".")
}
