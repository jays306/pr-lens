package analysis

import (
	"path"
	"regexp"
	"strings"
)

// importPatterns captures import paths from common languages found in diff lines.
// Each pattern returns one submatch: the raw import path string.
var importPatterns = []*regexp.Regexp{
	// Go:   import "pkg/foo" or import foo "pkg/foo"
	regexp.MustCompile(`(?m)^\+?\s*(?:\w+\s+)?"([^"]+)"`),
	// TypeScript / JavaScript:  from './foo' or from "../bar/baz"
	regexp.MustCompile(`(?m)from\s+['"]([^'"]+)['"]`),
	// TypeScript / JavaScript:  require('./foo') or require("../bar")
	regexp.MustCompile(`(?m)require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
	// Python:  from foo.bar import baz  or  import foo.bar
	regexp.MustCompile(`(?m)^\+?\s*(?:from\s+([\w./]+)\s+import|import\s+([\w./]+))`),
	// Ruby:  require 'foo/bar'  or  require_relative '../baz'
	regexp.MustCompile(`(?m)require(?:_relative)?\s+['"]([^'"]+)['"]`),
}

// ResolveXRefs finds files in repoTree that are referenced by the diff but are
// not already present in alreadyFetched. It returns a deduplicated list of
// paths (relative to the repo root) to fetch.
//
// The heuristic works in two steps:
//  1. Extract all raw import/require strings from added lines in the diff.
//  2. Try to match each raw string against entries in the repo tree using a
//     set of resolution strategies (exact match, suffix match, extension
//     inference, relative-path resolution anchored to the importing file).
func ResolveXRefs(diff string, diffFiles []string, repoTree []string, alreadyFetched map[string]bool) []string {
	rawRefs := extractRawRefs(diff)
	if len(rawRefs) == 0 || len(repoTree) == 0 {
		return nil
	}

	// Build a fast lookup set from the tree (exact path → true).
	treeSet := make(map[string]bool, len(repoTree))
	for _, p := range repoTree {
		treeSet[p] = true
	}

	// Collect the directories of the files touched by the diff so we can
	// resolve relative imports (e.g. "./utils" from "src/api/handler.ts").
	diffDirs := make(map[string]bool, len(diffFiles))
	for _, f := range diffFiles {
		diffDirs[path.Dir(f)] = true
	}

	resolved := make(map[string]bool)

	for ref := range rawRefs {
		// Skip stdlib-like or external module references we can't resolve.
		if looksExternal(ref) {
			continue
		}
		for _, candidate := range candidates(ref, diffDirs, repoTree, treeSet) {
			if !alreadyFetched[candidate] && !resolved[candidate] {
				resolved[candidate] = true
			}
		}
	}

	out := make([]string, 0, len(resolved))
	for p := range resolved {
		out = append(out, p)
	}
	return out
}

// extractRawRefs pulls all unique import/require strings from the diff.
func extractRawRefs(diff string) map[string]bool {
	refs := make(map[string]bool)
	for _, pat := range importPatterns {
		for _, m := range pat.FindAllStringSubmatch(diff, -1) {
			for _, sub := range m[1:] {
				sub = strings.TrimSpace(sub)
				if sub != "" {
					refs[sub] = true
				}
			}
		}
	}
	return refs
}

// looksExternal returns true if the reference is clearly an external module
// (no path separators and no leading dot) or a stdlib package we cannot
// resolve to a repo-local file.
func looksExternal(ref string) bool {
	// Relative import — always local.
	if strings.HasPrefix(ref, ".") {
		return false
	}
	// Contains a slash but starts with a known hosting domain prefix → external.
	for _, ext := range []string{"github.com/", "golang.org/", "gopkg.in/", "k8s.io/", "sigs.k8s.io/"} {
		if strings.HasPrefix(ref, ext) {
			return true
		}
	}
	// No slash → likely a single-segment stdlib or npm bare package.
	// Still try to match because monorepos sometimes use bare workspace names.
	// We'll let the candidate resolution discard it if nothing matches.
	return false
}

// candidates generates possible repo-relative file paths for a given raw ref.
func candidates(ref string, diffDirs map[string]bool, repoTree []string, treeSet map[string]bool) []string {
	var out []string
	add := func(p string) {
		p = path.Clean(p)
		if p != "" && p != "." && treeSet[p] {
			out = append(out, p)
		}
	}

	// 1. Exact match.
	add(ref)

	// 2. Try common extensions if the ref has none.
	if path.Ext(ref) == "" {
		for _, ext := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rb", ".java", ".kt", ".rs", ".swift"} {
			add(ref + ext)
		}
		// Also try index files (JS/TS convention).
		for _, idx := range []string{"/index.ts", "/index.tsx", "/index.js"} {
			add(ref + idx)
		}
	}

	// 3. Relative resolution: anchor the ref to each directory that has a
	//    changed file and apply the same extension inference.
	if strings.HasPrefix(ref, ".") {
		for dir := range diffDirs {
			resolved := path.Join(dir, ref)
			add(resolved)
			if path.Ext(ref) == "" {
				for _, ext := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rb"} {
					add(resolved + ext)
				}
				for _, idx := range []string{"/index.ts", "/index.tsx", "/index.js"} {
					add(resolved + idx)
				}
			}
		}
	}

	// 4. Suffix match: find any tree entry whose path ends with the ref as a
	//    path component. This handles Go sub-package references like
	//    "internal/config" matching "backend/internal/config/config.go".
	if len(out) == 0 {
		suffix := "/" + strings.TrimPrefix(ref, "/")
		for _, entry := range repoTree {
			if strings.HasSuffix(entry, suffix) || entry == strings.TrimPrefix(suffix, "/") {
				if treeSet[entry] {
					out = append(out, entry)
				}
			}
		}
	}

	return out
}
