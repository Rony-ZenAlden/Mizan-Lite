// Package notify computes what a user should be told, and remembers what they have dismissed.
//
// # Why notifications are RULES and not rows
//
// The obvious design is a `notifications` table: something detects a condition, writes a row, a
// screen reads it, the user dismisses it.
//
// That table is a projection of facts that live elsewhere — due dates, stock levels, job outcomes
// — with no rebuild, no verifier and no drift report. Phase 8 refused exactly that shape for
// reporting: *the moment a fact needs a third home, the second home was the wrong one.*
//
// Worse, a stored notification goes STALE. "Invoice 4471 is overdue" survives the invoice being
// paid, and a user told three times about something they already fixed stops reading
// notifications at all — which costs more than never having had them.
//
// So a notice is computed from the current state every time it is asked for, and a condition that
// resolves takes its notice with it. What genuinely needs storing is the DISMISSAL: "I have seen
// this and do not want to see it again" is a fact about the user, and lives nowhere else.
package notify

import (
	"context"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Severity is how much a notice matters.
type Severity string

// The severities, in the order a screen should present them.
const (
	// Danger is something that is wrong now: the books do not balance, a backup failed.
	Danger Severity = "danger"
	// Warning is something that will become wrong: an invoice is overdue, no backup in a week.
	Warning Severity = "warning"
	// Info is worth knowing and needs no action.
	Info Severity = "info"
)

var severityOrder = map[Severity]int{Danger: 0, Warning: 1, Info: 2}

// Notice is one thing a user should be told.
type Notice struct {
	// Key identifies WHAT this is about, stably across runs: "sales.overdue:<documentId>".
	//
	// # Why the key is derived and not generated
	//
	// A dismissal is stored against it, so a key that changed between runs would make dismissing
	// useless — the same condition would come back under a new name every time. It must be
	// derivable from the condition alone.
	Key string
	// Rule is which rule produced it, so a screen can group and a user can silence a whole kind.
	Rule string
	// Severity is how much it matters.
	Severity Severity
	// MessageKey is an i18n key; Params fills it. Never a rendered sentence: this package has no
	// locale, and putting one here would put i18n in two places.
	MessageKey string
	Params     map[string]string
	// EntityID is what to open, where there is something.
	EntityID id.ID
	// EntityKind routes the caller, as the module names it: "sales.invoice".
	EntityKind string
}

// Rule computes notices from the current state.
//
// One method. A rule that needed configuration takes it at construction, so this interface never
// grows a "settings" parameter that only two rules read.
type Rule interface {
	// Name identifies the rule, and prefixes the keys it produces.
	Name() string
	// Check returns what is true NOW. An empty result is the good case and not an error.
	Check(ctx context.Context, companyID id.ID) ([]Notice, error)
}

// Dismissals records what a user has chosen not to see again.
type Dismissals interface {
	// Dismissed returns the keys this user has dismissed for this company.
	Dismissed(ctx context.Context, companyID, userID id.ID) (map[string]bool, error)
	// Dismiss records one.
	Dismiss(ctx context.Context, companyID, userID id.ID, key string) error
	// Restore forgets a dismissal, so the notice returns if its condition still holds.
	Restore(ctx context.Context, companyID, userID id.ID, key string) error
}

// Centre runs every rule and filters what has been dismissed.
type Centre struct {
	rules      []Rule
	dismissals Dismissals
}

// New builds a Centre.
func New(dismissals Dismissals, rules ...Rule) *Centre {
	return &Centre{rules: rules, dismissals: dismissals}
}

// Rules lists the registered rule names, for a coverage test and for a screen that offers to
// silence a kind.
func (c *Centre) Rules() []string {
	out := make([]string, 0, len(c.rules))
	for _, rule := range c.rules {
		out = append(out, rule.Name())
	}
	return out
}

// Result is what a check produced.
type Result struct {
	Notices []Notice
	// Dismissed counts notices suppressed by a dismissal, so a screen can offer to show them
	// again. A user who dismissed something in error otherwise has no way back.
	Dismissed int
	// Failed names rules that could not run.
	//
	// A notification centre that goes silent because one rule broke is worse than one that says
	// so: silence reads as "nothing is wrong". Same choice `search.Response.Failed` makes.
	Failed []string
}

// Check runs every rule.
func (c *Centre) Check(ctx context.Context, companyID, userID id.ID) (Result, error) {
	dismissed, err := c.dismissals.Dismissed(ctx, companyID, userID)
	if err != nil {
		// A dismissal store that cannot be read means every notice would reappear, including
		// ones the user silenced. Failing is better than nagging somebody who already said no.
		return Result{}, err
	}

	result := Result{Notices: make([]Notice, 0, 8), Failed: []string{}}
	for _, rule := range c.rules {
		notices, ruleErr := rule.Check(ctx, companyID)
		if ruleErr != nil {
			result.Failed = append(result.Failed, rule.Name())
			continue
		}
		for _, notice := range notices {
			notice.Rule = rule.Name()
			// The rule's name PREFIXES its keys, so two rules cannot collide and a dismissal
			// cannot silence a different rule's notice by accident.
			if !strings.HasPrefix(notice.Key, rule.Name()+".") {
				notice.Key = rule.Name() + "." + notice.Key
			}
			if dismissed[notice.Key] {
				result.Dismissed++
				continue
			}
			result.Notices = append(result.Notices, notice)
		}
	}

	// Most serious first, then by key so two runs agree.
	sort.SliceStable(result.Notices, func(i, j int) bool {
		left, right := result.Notices[i], result.Notices[j]
		if left.Severity != right.Severity {
			return severityOrder[left.Severity] < severityOrder[right.Severity]
		}
		return left.Key < right.Key
	})
	return result, nil
}

// Dismiss records that a user does not want to see a notice again.
func (c *Centre) Dismiss(ctx context.Context, companyID, userID id.ID, key string) error {
	return c.dismissals.Dismiss(ctx, companyID, userID, key)
}

// Restore forgets a dismissal.
func (c *Centre) Restore(ctx context.Context, companyID, userID id.ID, key string) error {
	return c.dismissals.Restore(ctx, companyID, userID, key)
}
