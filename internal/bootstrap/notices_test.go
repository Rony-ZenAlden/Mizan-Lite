package bootstrap_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/platform/backup"
)

// TestTheNoticeCentreRunsEveryRuleAgainstTheRealApplication
//
// The package's own tests use stubs, which prove the fan-out, the dismissal filter and the
// ordering — and say nothing about whether any rule works, or whether the composition root
// registered it. Both are the failure this is written against.
func TestTheNoticeCentreRunsEveryRuleAgainstTheRealApplication(t *testing.T) {
	app, ctx, companyID := traded(t)

	if app.Notices == nil {
		t.Fatal("the composition root built no notice centre")
	}
	rules := app.Notices.Rules()
	if len(rules) < 4 {
		t.Errorf("%d rules registered (%v), want the four this build ships", len(rules), rules)
	}

	result, err := app.Notices.Check(ctx, companyID, id.ID("some-user"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// Every rule ran. A rule that errors is reported rather than silent, so an empty Failed is
	// the assertion that all four could answer against a real database.
	if len(result.Failed) != 0 {
		t.Errorf("rules failed against a real company: %v", result.Failed)
	}
}

// TestAFreshInstallIsToldItHasNoBackup
//
// The one notice a brand-new shop should see, and the one that matters most: a company trading
// for a week with no backup is one disk failure from having no business records.
func TestAFreshInstallIsToldItHasNoBackup(t *testing.T) {
	app, ctx, companyID := traded(t)

	result, err := app.Notices.Check(ctx, companyID, id.ID("some-user"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	var told bool
	for _, notice := range result.Notices {
		if notice.Rule == "ops.backup" {
			told = true
			if notice.MessageKey != "notices.backup.never" {
				t.Errorf("message = %q, want notices.backup.never", notice.MessageKey)
			}
		}
	}
	if !told {
		t.Error("a company that has never been backed up is told nothing")
	}

	// Taking one silences it — because the CONDITION resolved, not because anything was
	// dismissed. That distinction is the whole design.
	if _, err = app.Backups.Take(ctx, backup.OnDemand); err != nil {
		t.Fatalf("Take: %v", err)
	}
	result, err = app.Notices.Check(ctx, companyID, id.ID("some-user"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, notice := range result.Notices {
		if notice.Rule == "ops.backup" {
			t.Errorf("still warned about backups after taking one: %+v", notice)
		}
	}
	if result.Dismissed != 0 {
		t.Errorf("the notice was DISMISSED (%d) rather than resolved — a condition that goes "+
			"away should take its notice with it, not need silencing", result.Dismissed)
	}
}

// TestADismissalSurvivesAndIsPerUser
//
// One person deciding they do not need to see a warning must not silence it for the owner. The
// two have different jobs and different reasons to look.
func TestADismissalSurvivesAndIsPerUser(t *testing.T) {
	app, ctx, companyID := traded(t)

	// REAL users, because `notice_dismissals.user_id` is a foreign key — which is the schema
	// refusing to store a preference for somebody who does not exist. The first version of this
	// test used made-up ids and the constraint caught it.
	operatorID := newUser(t, app, ctx, companyID, "till", "Till operator")
	ownerID := newUser(t, app, ctx, companyID, "owner", "The owner")

	result, err := app.Notices.Check(ctx, companyID, operatorID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Notices) == 0 {
		t.Skip("no notices to dismiss on this build")
	}
	key := result.Notices[0].Key

	if err = app.Notices.Dismiss(ctx, companyID, operatorID, key); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}

	// Dismissing twice is not an error: two windows open on one screen is ordinary.
	if err = app.Notices.Dismiss(ctx, companyID, operatorID, key); err != nil {
		t.Fatalf("dismissing twice: %v", err)
	}

	operator, err := app.Notices.Check(ctx, companyID, operatorID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, notice := range operator.Notices {
		if notice.Key == key {
			t.Errorf("the dismissed notice %q is still shown to the user who dismissed it", key)
		}
	}

	// The OWNER still sees it.
	owner, err := app.Notices.Check(ctx, companyID, ownerID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var ownerSees bool
	for _, notice := range owner.Notices {
		if notice.Key == key {
			ownerSees = true
		}
	}
	if !ownerSees {
		t.Errorf("one user dismissing %q silenced it for everybody", key)
	}
}

// newUser creates a user the dismissals table will accept.
func newUser(
	t *testing.T, app *bootstrap.App, ctx context.Context, companyID id.ID,
	username, display string,
) id.ID {
	t.Helper()
	if err := app.Identity.SeedRoles(ctx, companyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	user, err := app.Identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: companyID, Username: username, DisplayName: display,
		Password: "a sufficiently long passphrase", IsSystem: true,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return user.ID
}

// TestAJobIsReportedOnlyWhileItsMostRecentRunFailed
//
// # The rule that reports something that HAPPENED
//
// Every other rule reads a condition that IS: the books are out, no backup exists. A failed job
// is over by the time anybody looks — which is the gap 9.5's design named, and the job history is
// the answer, because a rule over that history is still derived from current state.
//
// What makes it a rule rather than a stored message is the word MOST RECENT. A job that failed at
// midnight and succeeded at one o'clock is working, and reporting it would teach the reader that
// these notices do not mean anything — which is how a notification centre becomes wallpaper.
func TestAJobIsReportedOnlyWhileItsMostRecentRunFailed(t *testing.T) {
	app, ctx, companyID := traded(t)
	userID := newUser(t, app, ctx, companyID, "ops", "Operator")

	// Reconcile puts the declared jobs in the table, which is what the scheduler does at start
	// when it is running. The test drives it directly, as every other job test here does.
	if _, err := app.Scheduler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var jobID string
	if err := app.DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT id FROM jobs WHERE job_key = ?`, bootstrap.KeyScheduledBackup,
	).Scan(&jobID); err != nil {
		t.Fatalf("finding the backup job: %v", err)
	}

	// A failed run, written straight to the table. There is no service method that makes a job
	// fail on demand — correctly — and this is the state the rule exists to notice.
	insertRun(t, app, ctx, jobID, "failed", "2026-08-16T01:00:00Z")

	result, err := app.Notices.Check(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var reported bool
	for _, notice := range result.Notices {
		if notice.Rule == "ops.jobs" && notice.Params["job"] == bootstrap.KeyScheduledBackup {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("a failed job was not reported: %+v", result.Notices)
	}

	// A later SUCCESS resolves it. Not a dismissal — the condition itself is gone.
	insertRun(t, app, ctx, jobID, "succeeded", "2026-08-16T02:00:00Z")

	result, err = app.Notices.Check(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, notice := range result.Notices {
		if notice.Rule == "ops.jobs" && notice.Params["job"] == bootstrap.KeyScheduledBackup {
			t.Errorf("a job that failed once and then succeeded is still reported: %+v", notice)
		}
	}
	if result.Dismissed != 0 {
		t.Errorf("the notice was dismissed (%d) rather than resolved", result.Dismissed)
	}
}

// insertRun records a job outcome directly.
func insertRun(
	t *testing.T, app *bootstrap.App, ctx context.Context, jobID, status, at string,
) {
	t.Helper()
	runID, err := id.New()
	if err != nil {
		t.Fatalf("id.New: %v", err)
	}
	if _, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO job_runs (id, job_id, status, attempt, started_at, finished_at)
		VALUES (?, ?, ?, 1, ?, ?)`,
		string(runID), jobID, status, at, at); err != nil {
		t.Fatalf("recording a %s run: %v", status, err)
	}
}
