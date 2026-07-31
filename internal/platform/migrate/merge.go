package migrate

import (
	"io/fs"
	"sort"
)

// Merge combines several migration filesystems into one, so a module can own its schema files
// while version numbers stay globally ordered (ARCHITECTURE_v1 §10.3).
//
// This is what Step 0.4 carried forward: Load reads a single FS, and merging was deferred
// until a second source existed. Step 0.9 is that moment — the currency module embeds
// 0003_currency.sql in its own package, which is the only way `Module.Migrations() fs.FS`
// can mean anything.
//
// Duplicate versions across sources are NOT detected here: Load already fails on them, and
// doing it in one place means a module colliding with the platform and two modules colliding
// with each other produce the identical error.
func Merge(sources ...fs.FS) fs.FS { return mergedFS(sources) }

type mergedFS []fs.FS

// Open returns the first source that has the file. Order is the caller's, so the platform's
// own migrations are searched before any module's.
func (m mergedFS) Open(name string) (fs.File, error) {
	var lastErr error
	for _, src := range m {
		f, err := src.Open(name)
		if err == nil {
			return f, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return nil, lastErr
}

// ReadDir concatenates the entries of every source.
//
// A name appearing in two sources is returned once. That is not a way to hide a duplicate
// version: two files claiming version 0003 would have different names (0003_currency.sql and
// 0003_something.sql) and Load rejects them. Identical names are the same file.
func (m mergedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	seen := map[string]bool{}
	var out []fs.DirEntry
	var lastErr error

	for _, src := range m {
		entries, err := fs.ReadDir(src, name)
		if err != nil {
			lastErr = err
			continue
		}
		for _, e := range entries {
			if seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			out = append(out, e)
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}
