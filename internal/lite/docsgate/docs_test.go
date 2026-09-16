// Package docsgate holds Mizan Lite's documentation to where the owner asked it to live.
//
// The instruction (2026-09-13, requirement R3): every Lite document — design, decisions, phase design notes
// and records, progress — lives in docs/mizan_lite/ and is kept current. A rule written only in a README is
// a rule the next document ignores; this makes it a failing test.
package docsgate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const liteDocs = "docs/mizan_lite"

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// markdownIn lists .md files under dir, relative to dir, with forward slashes.
func markdownIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

func TestEveryLiteDocumentIsInItsFolderAndIndexed(t *testing.T) {
	root := filepath.Join(repoRoot(t), filepath.FromSlash(liteDocs))
	docs := markdownIn(t, root)

	for _, required := range []string{"README.md", "PROGRESS.md", "DESIGN.md", "DECISIONS.md", "phases/L0_SKELETON.md"} {
		if !contains(docs, required) {
			t.Errorf("%s/%s is missing", liteDocs, required)
		}
	}

	index, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if doc == "README.md" {
			continue
		}
		if !strings.Contains(string(index), "("+doc+")") {
			t.Errorf("%s/%s exists but README.md does not link it — an unindexed document is one nobody finds", liteDocs, doc)
		}
	}
}

func TestEveryLinkBetweenLiteDocumentsResolves(t *testing.T) {
	root := filepath.Join(repoRoot(t), filepath.FromSlash(liteDocs))
	link := regexp.MustCompile(`\]\(([^)#\s]+\.md)(?:#[^)]*)?\)`)
	checked := 0
	for _, doc := range markdownIn(t, root) {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(doc)))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range link.FindAllStringSubmatch(string(raw), -1) {
			if strings.Contains(m[1], "://") {
				continue
			}
			checked++
			target := filepath.Join(root, filepath.Dir(filepath.FromSlash(doc)), filepath.FromSlash(m[1]))
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s links %s, which does not exist", doc, m[1])
			}
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d links; the pattern is not matching", checked)
	}
}

// TestNoLiteDocumentLivesOutsideItsFolder catches the drift this gate exists for: a phase record or note
// written into docs/architecture/ (where Lite's first two documents were drafted) or anywhere else.
func TestNoLiteDocumentLivesOutsideItsFolder(t *testing.T) {
	root := repoRoot(t)
	named := regexp.MustCompile(`(?i)(^|/)(mizan[_-]?lite|lite[_-])[^/]*\.md$`)
	var strays []string
	for _, doc := range markdownIn(t, filepath.Join(root, "docs")) {
		if strings.HasPrefix(doc, "mizan_lite/") {
			continue
		}
		if named.MatchString(doc) {
			strays = append(strays, "docs/"+doc)
		}
	}
	if len(strays) > 0 {
		t.Fatalf("Lite documents outside %s: %v", liteDocs, strays)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestTheGuidesHaveTheSameSectionsInBothLanguages (L8 D-L8.16): the shop's documents are written in Arabic first and followed in
// English. A section added to one and forgotten in the other leaves a reader without it, which is how a translation rots.
func TestTheGuidesHaveTheSameSectionsInBothLanguages(t *testing.T) {
	root := filepath.Join(repoRoot(t), filepath.FromSlash(liteDocs), "guide")
	levels := func(name string) []int {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		var out []int
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "#") {
				out = append(out, len(line)-len(strings.TrimLeft(line, "#")))
			}
		}
		return out
	}
	for _, pair := range [][2]string{
		{"SHOP_GUIDE.ar.md", "SHOP_GUIDE.md"},
		{"INSTALL.ar.md", "INSTALL.md"},
		{"TROUBLESHOOTING.ar.md", "TROUBLESHOOTING.md"},
	} {
		arabic, english := levels(pair[0]), levels(pair[1])
		if len(arabic) != len(english) {
			t.Errorf("%s has %d headings and %s has %d", pair[0], len(arabic), pair[1], len(english))
			continue
		}
		for i := range arabic {
			if arabic[i] != english[i] {
				t.Errorf("%s and %s differ at heading %d: level %d against %d", pair[0], pair[1], i+1, arabic[i], english[i])
			}
		}
	}
}
