// Package seeds discovers JSON seed documents from layered filesystems.
//
// It closes the item Step 0.5 (D7) deferred and 0.9 and 0.12 carried forward: "JSON seed-file
// discovery and ordering across modules", parked each time because no seed was genuinely
// file-shaped. Country and business profiles are — numerous, edited by non-programmers, and
// expected to grow without a release (Addendum §C: "adding a country is dropping in a JSON
// file — no code, no release").
//
// "No release" is the load-bearing requirement. It is why discovery reads LAYERS: what the
// binary ships, and what an administrator has dropped into the data directory.
//
// # What this package does not do
//
// It does not decode a document. A country profile is a module's vocabulary, and platform must
// never learn one (`platform-independent-of-modules`). This package finds files, orders them,
// resolves overrides, bounds their size, and reports what it could not use. Decode is generic
// over the caller's type and knows nothing about it.
package seeds

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeUnreadable  = "seeds.unreadable"
	CodeTooLarge    = "seeds.too_large"
	CodeInvalidSeed = "seeds.invalid"
	CodeShippedSeed = "seeds.shipped_invalid"
)

// MaxFileBytes bounds one seed document.
//
// A seed document is a page of JSON. Anything larger is a mistake or a hostile file, and
// neither should be read into memory unbounded — the data directory is writable by anyone who
// can reach the customer's machine.
const MaxFileBytes = 1 << 20 // 1 MiB

// Origin says where a file came from, which decides how a failure is handled (§2.3).
type Origin string

const (
	// OriginShipped is embedded in the binary. Ours. A failure here is a build bug.
	OriginShipped Origin = "shipped"
	// OriginUser is the customer's data directory. Theirs. A failure here must not stop work.
	OriginUser Origin = "user"
)

// Layer is one filesystem to search, with the trust that comes from where it came from.
type Layer struct {
	FS     fs.FS
	Origin Origin
}

// File is one discovered document.
type File struct {
	// Path is slash-separated and relative to the searched directory.
	Path string
	// Name is the base filename without ".json" — and it is the IDENTITY. A later layer's
	// sy.json replaces an earlier layer's, whole.
	Name string
	Data []byte
	// Origin is the layer it came from. Carried through to Problem, and worth surfacing in
	// diagnostics: "which of these is the customer's?" is the first support question.
	Origin Origin
	// ReplacedShipped records that this user file shadowed one we ship. Support cannot
	// reproduce a customer's behaviour without knowing this happened.
	ReplacedShipped bool
}

// Problem is a file that could not be used, and why.
type Problem struct {
	Path   string
	Origin Origin
	Err    error
}

// Set is the outcome of discovery.
type Set struct {
	// Files are ordered by Name, deterministically. A seed run must apply the same documents
	// in the same order on every machine, or two installs of the same version differ.
	Files []File
	// Problems are files that were found but could not be read.
	Problems []Problem
}

// Discover reads dir from each layer in order, later layers replacing earlier ones by Name.
//
// Replacement is WHOLE-FILE, never a merge. A merged document would be half ours and half the
// customer's, and nobody — least of all a support engineer three years from now — could say
// which half produced a given behaviour. "This file, from this layer" is legible; a merge is
// not.
//
// A layer whose FS is nil is skipped, so a caller with no user directory passes nothing
// special. A directory that does not exist in a layer is not an error: shipping no profiles of
// a given kind is a legitimate state, and so is a customer who has never added one.
func Discover(dir string, layers ...Layer) (Set, error) {
	if dir == "" {
		dir = "."
	}

	// Keyed by Name so a later layer replaces an earlier one; the map is ordered at the end.
	found := map[string]File{}
	var problems []Problem

	for _, layer := range layers {
		if layer.FS == nil {
			continue
		}
		entries, err := fs.ReadDir(layer.FS, dir)
		if err != nil {
			// Absent is fine — see above. Anything else is a real read failure, and for a
			// shipped layer that means the embed directive and this call disagree.
			if isNotExist(err) {
				continue
			}
			if layer.Origin == OriginShipped {
				return Set{}, errs.Wrap(err, errs.CategoryInternal, CodeUnreadable,
					"a shipped seed directory could not be read").WithParam("dir", dir)
			}
			problems = append(problems, Problem{Path: dir, Origin: layer.Origin, Err: err})
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			full := path.Join(dir, entry.Name())
			data, readErr := readBounded(layer.FS, full)
			if readErr != nil {
				if layer.Origin == OriginShipped {
					return Set{}, readErr
				}
				problems = append(problems, Problem{Path: full, Origin: layer.Origin, Err: readErr})
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			_, shadowed := found[name]
			found[name] = File{
				Path: full, Name: name, Data: data, Origin: layer.Origin,
				ReplacedShipped: shadowed && layer.Origin == OriginUser,
			}
		}
	}

	files := make([]File, 0, len(found))
	for _, f := range found {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	return Set{Files: files, Problems: problems}, nil
}

// readBounded reads a file, refusing anything over MaxFileBytes.
//
// Enforced WHILE READING rather than by asking the entry how big it is: a stat can be stale or
// wrong, and the bound exists precisely for a file we do not trust.
func readBounded(fsys fs.FS, name string) ([]byte, error) {
	file, err := fsys.Open(name)
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeUnreadable,
			"a seed file could not be opened").WithParam("file", name)
	}
	defer func() { _ = file.Close() }()

	var buf bytes.Buffer
	// MaxFileBytes+1 so a file exactly at the limit reads fully and one byte over is caught.
	written, err := buf.ReadFrom(newLimited(file, MaxFileBytes+1))
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeUnreadable,
			"a seed file could not be read").WithParam("file", name)
	}
	if written > MaxFileBytes {
		return nil, errs.Validation(CodeTooLarge,
			"a seed file is larger than the permitted size").WithParam("file", name)
	}
	return buf.Bytes(), nil
}

// Doc is a decoded document with the file it came from.
type Doc[T any] struct {
	File  File
	Value T
}

// Result is the outcome of decoding a Set.
type Result[T any] struct {
	Docs     []Doc[T]
	Problems []Problem
}

// Decode decodes every file in set, applying the shipped-is-fatal rule.
//
// # The asymmetry, which is the whole rule
//
// It lives HERE, once, rather than in each module, because it is exactly the kind of policy
// that four call sites would implement three ways:
//
//   - A broken SHIPPED file is a bug in our build. It fails loudly — on a developer's machine,
//     in CI — and never silently on a customer's. An error is returned and nothing is loaded.
//   - A broken USER file is a typo by an administrator at 11pm. It is reported and skipped, and
//     whatever it was replacing stays in force. A seed file must not be able to close a shop.
//
// The returned error is therefore ONLY ever about a file we shipped.
func Decode[T any](set Set, decode func([]byte, *T) error) (Result[T], error) {
	out := Result[T]{Problems: append([]Problem(nil), set.Problems...)}

	for _, file := range set.Files {
		var value T
		if err := decode(file.Data, &value); err != nil {
			if file.Origin == OriginShipped {
				return Result[T]{}, errs.Wrap(err, errs.CategoryInternal, CodeShippedSeed,
					"a seed file shipped with this build is invalid").
					WithParam("file", file.Path)
			}
			out.Problems = append(out.Problems, Problem{
				Path: file.Path, Origin: file.Origin,
				Err: errs.Wrap(err, errs.CategoryValidation, CodeInvalidSeed,
					"a seed file could not be read").WithParam("file", file.Path),
			})
			continue
		}
		out.Docs = append(out.Docs, Doc[T]{File: file, Value: value})
	}
	return out, nil
}

// StrictJSON decodes exactly, rejecting unknown fields and trailing content.
//
// Strict on purpose. A profile whose key is misspelt — "default_locale" typed with the letters
// transposed — must be REPORTED, not quietly ignored: a silently-ignored key is a value the
// customer believes they configured and that the system never saw. That is the worst class of
// configuration bug, invisible from both ends.
func StrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errs.Wrap(err, errs.CategoryValidation, CodeInvalidSeed, "the document is not valid")
	}
	// A second value in the same file means the author appended rather than replaced, and one
	// of the two documents would be silently lost.
	if dec.More() {
		return errs.Validation(CodeInvalidSeed, "the document has trailing content")
	}
	return nil
}

// isNotExist reports a missing directory, which is not a failure.
//
// A module may ship no profiles of a kind, and a customer may never have created the overlay
// directory. Both are ordinary.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// newLimited bounds a reader. io.LimitReader would do, but naming it here keeps the intent —
// "this file is not allowed to be big" — next to the constant that says how big.
func newLimited(r io.Reader, n int64) io.Reader { return io.LimitReader(r, n) }
