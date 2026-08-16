package buildinfo_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTheReadingOrderNamesDocumentsThatExist
//
// # DoD criterion 9, and the failure it prevents
//
// The README's job is to give a reader arriving at thirty-seven design documents an order to read
// them in. A link to a document that has been renamed or removed sends them to a 404 on their
// first minute — and nothing else in this repository would notice.
//
// The check is a link walk, not a word count. It says the entry point still points at things.
func TestTheReadingOrderNamesDocumentsThatExist(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}

	// Markdown links to files in the repository. External links are somebody else's problem.
	links := regexp.MustCompile(`\]\((docs/[^)#]+|tools/[^)#]+|ci/[^)#]+)\)`)
	found := links.FindAllStringSubmatch(string(raw), -1)

	if len(found) < 6 {
		t.Fatalf("the README links to %d repository documents; a reading order needs more than "+
			"that, so this check is looking at the wrong file", len(found))
	}

	for _, match := range found {
		target := strings.TrimSuffix(match[1], "/")
		if _, statErr := os.Stat(filepath.Join(root, target)); statErr != nil {
			t.Errorf("the README links to %s, which does not exist — a reader's first minute is "+
				"spent on a broken link", target)
		}
	}
}

// TestTheReadmeDoesNotClaimThePhaseItIsNoLongerIn
//
// # A stale entry point is worse than none
//
// The README said "Phase 0 — Foundation. No business features yet" until Step 10.5 — ten phases
// after that stopped being true. A reader arriving would have concluded the project was a kernel
// with nothing on top of it.
//
// Nothing catches that automatically in general. What CAN be caught is the specific claim, so it
// cannot come back by a careless revert.
func TestTheReadmeDoesNotClaimThePhaseItIsNoLongerIn(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	body := string(raw)

	for _, stale := range []string{
		"No business features yet",
		"**Phase 0 — Foundation (application kernel).**",
	} {
		if strings.Contains(body, stale) {
			t.Errorf("the README still says %q, which stopped being true ten phases ago", stale)
		}
	}

	// And it names the guides, which are the only documents written for somebody who is not a
	// programmer. A repository that buries them has them for nobody.
	for _, guide := range []string{
		"docs/guide/GETTING_STARTED.md",
		"docs/guide/GETTING_STARTED.ar.md",
	} {
		if !strings.Contains(body, guide) {
			t.Errorf("the README does not link to %s", guide)
		}
		if _, statErr := os.Stat(filepath.Join(repoRoot(t), guide)); statErr != nil {
			t.Errorf("%s is missing: %v", guide, statErr)
		}
	}
}

// TestEveryPhaseHasADesignDocument
//
// Ten phases, ten documents, and the reading order promises all of them. A phase whose document
// was never written is a phase whose reasoning exists only in commit messages.
func TestEveryPhaseHasADesignDocument(t *testing.T) {
	root := filepath.Join(repoRoot(t), "docs", "architecture")

	for _, phase := range []string{
		"PHASE_0_FOUNDATION.md",
		"PHASE_1_CORE_DATA.md",
		"PHASE_2_FINANCIAL_SPINE.md",
		"PHASE_3_MASTER_DATA.md",
		"PHASE_4_INVENTORY.md",
		"PHASE_5_SALES_POS.md",
		"PHASE_6_PURCHASING.md",
		"PHASE_7_MONEY_OUT.md",
		"PHASE_8_INSIGHT.md",
		"PHASE_9_OPERATIONS.md",
		"PHASE_10_POLISH.md",
	} {
		info, err := os.Stat(filepath.Join(root, phase))
		if err != nil {
			t.Errorf("%s is missing: %v", phase, err)
			continue
		}
		// A stub counts as missing. Every real phase document records an analysis, its decisions,
		// what it declined to do, and a DoD review — none of which fits in a page.
		if info.Size() < 4000 {
			t.Errorf("%s is %d bytes, which is too short to hold a phase's reasoning",
				phase, info.Size())
		}
	}
}
