package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Migration is one immutable, numbered schema change.
type Migration struct {
	Version  int64  // globally sequenced; ordering is by this number
	Name     string // from the filename, e.g. "platform"
	FileName string
	SQL      string
	Checksum string // SHA-256 hex of SQL — the anti-divergence guarantee (§3.3)
}

// Statements that must never appear inside a migration. PRAGMAs are connection state
// (set by the DSN) and several cannot run inside a transaction; VACUUM and ATTACH
// likewise escape or break the per-migration transaction. Catching these at load time
// means the failure happens on a developer's machine, never on a customer's.
var forbiddenStatements = []string{"pragma", "vacuum", "attach", "detach"}

// Load reads, validates, and orders every migration in fsys.
//
// Files must be named <version>_<name>.sql, e.g. "0001_platform.sql". Version numbers
// are globally sequenced across modules; a duplicate version is a developer error and
// fails here rather than producing a nondeterministic apply order.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed, "reading migrations directory")
	}

	var out []Migration
	seen := make(map[int64]string)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m, err := parseFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[m.Version]; dup {
			return nil, errs.Validation(CodeDuplicateVersion,
				fmt.Sprintf("duplicate migration version %d: %q and %q", m.Version, prev, m.FileName))
		}
		seen[m.Version] = m.FileName
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func parseFile(fsys fs.FS, name string) (Migration, error) {
	base := path.Base(name)
	stem := strings.TrimSuffix(base, ".sql")

	idx := strings.Index(stem, "_")
	if idx <= 0 {
		return Migration{}, errs.Validation(CodeBadFileName,
			fmt.Sprintf("migration %q must be named <version>_<name>.sql", base))
	}
	version, err := strconv.ParseInt(stem[:idx], 10, 64)
	if err != nil || version <= 0 {
		return Migration{}, errs.Validation(CodeBadFileName,
			fmt.Sprintf("migration %q has a non-numeric or non-positive version", base))
	}

	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Migration{}, errs.Wrap(err, errs.CategoryInternal, CodeLoadFailed,
			"reading migration "+base)
	}
	sqlText := string(raw)
	if err := validateContent(base, sqlText); err != nil {
		return Migration{}, err
	}

	sum := sha256.Sum256(raw)
	return Migration{
		Version:  version,
		Name:     stem[idx+1:],
		FileName: base,
		SQL:      sqlText,
		Checksum: hex.EncodeToString(sum[:]),
	}, nil
}

// validateContent rejects statements that would break per-migration atomicity.
//
// The check is on the leading keyword of every statement, not on line prefixes: an
// earlier version scanned line-by-line and so accepted
// "CREATE TABLE t (...); PRAGMA journal_mode = DELETE;" — one line, two statements, the
// dangerous one invisible.
func validateContent(fileName, sqlText string) error {
	for _, stmt := range splitStatements(sqlText) {
		lead := strings.ToLower(leadingKeyword(stmt))
		if lead == "" {
			continue
		}
		for _, bad := range forbiddenStatements {
			if lead == bad {
				return errs.Validation(CodeForbiddenStatement, fmt.Sprintf(
					"migration %s contains a forbidden %q statement: it cannot run inside the "+
						"per-migration transaction", fileName, bad))
			}
		}
	}
	return nil
}

// splitStatements splits SQL into top-level statements, discarding comments.
//
// It tracks quoting and comment state rather than splitting naively on ';', because a
// semicolon inside a string literal or a comment does not end a statement. Getting this
// wrong would either reject a valid migration or, worse, let a forbidden statement past
// validateContent.
func splitStatements(sqlText string) []string {
	var (
		out  []string
		cur  strings.Builder
		i    int
		n    = len(sqlText)
		emit = func() {
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	)
	for i < n {
		c := sqlText[i]
		switch {
		// Line comment: skip to end of line.
		case c == '-' && i+1 < n && sqlText[i+1] == '-':
			for i < n && sqlText[i] != '\n' {
				i++
			}
		// Block comment: skip to the closing delimiter.
		case c == '/' && i+1 < n && sqlText[i+1] == '*':
			i += 2
			for i+1 < n && !(sqlText[i] == '*' && sqlText[i+1] == '/') {
				i++
			}
			i += 2
		// String literal: copy verbatim, honouring the '' escape.
		case c == '\'':
			cur.WriteByte(c)
			i++
			for i < n {
				if sqlText[i] == '\'' {
					if i+1 < n && sqlText[i+1] == '\'' { // escaped quote
						cur.WriteString("''")
						i += 2
						continue
					}
					cur.WriteByte('\'')
					i++
					break
				}
				cur.WriteByte(sqlText[i])
				i++
			}
		case c == ';':
			emit()
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	emit() // trailing statement without a terminating ';'
	return out
}

// leadingKeyword returns the first bare word of a statement.
func leadingKeyword(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	for i := 0; i < len(stmt); i++ {
		c := stmt[i]
		isWord := c == '_' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isWord {
			return stmt[:i]
		}
	}
	return stmt
}
