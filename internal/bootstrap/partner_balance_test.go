package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
)

// traded provisions a company with a chart, so a test about balances has books to compare with.
//
// # Why this helper exists rather than a skip
//
// The first version of these tests skipped when the application had no company — and both of them
// skipped on every run, silently. They looked green and asserted nothing, which is the failure
// mode this project keeps finding in its own tests: a check that cannot fail is worse than none,
// because it occupies the place where a real one would go.
//
// A test about partner balances needs a provisioned company. If provisioning breaks, this should
// FAIL loudly rather than quietly declining to run.
func traded(t *testing.T) (*bootstrap.App, context.Context, id.ID) {
	t.Helper()
	app := boot(t)
	ctx := app.Context()

	provisioned, err := app.Org.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "BAL", Name: "Balance Co", CountryCode: "SA", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err = app.Accounting.ApplyChart(ctx, provisioned.CompanyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	return app, ctx, provisioned.CompanyID
}

// TestPartnerBalancesReconcileToTheGeneralLedger
//
// # §7.13 invariant 5, and why it can only be tested HERE
//
//	"Partner balances equal the sum of their open documents minus settlements."
//
// The two sides are computed by completely different paths. The subsidiary ledger sums documents
// and their allocations across sales, purchasing and expenses; the control account sums journal
// lines written by Phase 2's posting rules. Nothing in a single module can compare them, because
// no module may import another — which is why the comparison lives behind ports satisfied in the
// composition root, and why the test lives where the root is assembled.
//
// When they disagree, one of them is wrong in a way nothing else will surface: an invoice posted
// against the wrong partner, a payment allocated across companies, a rule edited after the fact.
func TestPartnerBalancesReconcileToTheGeneralLedger(t *testing.T) {
	app, ctx, companyID := traded(t)

	// A partner who has traded nothing owes nothing, and the ledger agrees. That is the state
	// the verifier must call clean — one that reported drift on an untraded partner would be a
	// verifier nobody reads.
	created, err := app.Partner.CreatePartner(ctx, partner.NewPartnerInput{
		CompanyID: companyID, Code: "QUIET", Name: "Quiet Trading", IsCustomer: true,
	})
	if err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}

	balance, err := app.Partner.BalanceOf(ctx, companyID, created.ID)
	if err != nil {
		t.Fatalf("BalanceOf: %v", err)
	}
	if balance.ReceivableMinor != 0 || balance.PayableMinor != 0 || balance.NetMinor != 0 {
		t.Errorf("an untraded partner has a balance: %+v", balance)
	}

	discrepancies, err := app.Partner.VerifyBalances(ctx, companyID)
	if err != nil {
		t.Fatalf("VerifyBalances: %v", err)
	}
	if len(discrepancies) != 0 {
		t.Errorf("an untraded company reports %d discrepancies: %+v",
			len(discrepancies), discrepancies)
	}
}

// TestADraftJournalEntryCountsTowardNoPartnerBalance
//
// # The third unreachable guard of exactly this shape
//
// Purchasing's settled-amount query (6.7), expenses' settlement sum (7.2), and now the partner
// control balance: a `status = 'posted'` filter that the service's own API cannot exercise,
// because posting happens in the same transaction that creates the row.
//
// The pattern now has a name: **any service that creates and commits a document in one call has a
// "drafts do not count" filter its own API cannot reach.** All three are asserted the same way —
// by writing a draft straight to the table.
//
// It matters most here. A draft counted into a control balance would make the partner-balance
// verifier report a discrepancy against documents that correctly ignore it, so the one report
// designed to find real drift would cry wolf and the genuine drift would be lost in the noise.
func TestADraftJournalEntryCountsTowardNoPartnerBalance(t *testing.T) {
	app, ctx, companyID := traded(t)

	account, err := app.Accounting.AccountByCode(ctx, companyID, "1200")
	if err != nil {
		t.Fatalf("AccountByCode: %v", err)
	}

	var periodID string
	if err = app.DB.Reader(ctx).QueryRowContext(ctx, `
		SELECT p.id FROM fiscal_periods p
		  JOIN fiscal_years y ON y.id = p.fiscal_year_id
		 WHERE y.company_id = ? LIMIT 1`, string(companyID)).Scan(&periodID); err != nil {
		t.Fatalf("reading a fiscal period: %v", err)
	}

	created, err := app.Partner.CreatePartner(ctx, partner.NewPartnerInput{
		CompanyID: companyID, Code: "DRAFTCHK", Name: "Draft Check", IsCustomer: true,
	})
	if err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}

	entryID, _ := id.New()
	lineID, _ := id.New()
	now := clock.Format(clock.System().Now())

	if _, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO journal_entries (
			id, company_id, entry_number, entry_date, fiscal_period_id, status, source_module,
			created_at, updated_at, row_version
		) VALUES (?, ?, 'DRAFT-CHK', '2026-10-01', ?, 'draft', 'test', ?, ?, 1)`,
		string(entryID), string(companyID), periodID, now, now); err != nil {
		t.Fatalf("inserting a draft entry: %v", err)
	}
	if _, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO journal_lines (
			id, journal_entry_id, line_number, account_id, debit_minor, credit_minor,
			currency_code, exchange_rate_micro, partner_id, created_at
		) VALUES (?, ?, 1, ?, 500000, 0, 'SYP', 1000000, ?, ?)`,
		string(lineID), string(entryID), string(account.ID),
		string(created.ID), now); err != nil {
		t.Fatalf("inserting a draft line: %v", err)
	}

	balance, err := app.Accounting.PartnerControlBalance(ctx, companyID, created.ID, "AR")
	if err != nil {
		t.Fatalf("PartnerControlBalance: %v", err)
	}
	if balance != 0 {
		t.Fatalf("a draft entry contributed %d to a control balance", balance)
	}
}

// TestAVerifierWithNoControlLedgerRefusesRatherThanPassing
//
// A verifier built without the thing it verifies against would report "no discrepancies" for every
// company — the most dangerous possible answer, because it is indistinguishable from everything
// being right.
func TestAVerifierWithNoControlLedgerRefusesRatherThanPassing(t *testing.T) {
	svc := partner.NewService(nil, partner.Options{})

	if _, err := svc.VerifyBalances(context.Background(), id.ID("any")); err == nil {
		t.Fatal("a verifier with nothing to verify against reported success")
	}
}

// TestTheVerifierFindsDriftBetweenDocumentsAndTheLedger
//
// # A verifier tested only on a clean company is not tested
//
// The first version of this suite checked that an untraded company reports no discrepancies. That
// is worth asserting — a verifier that cried wolf on an empty company would be one nobody reads —
// but on its own it is a test that passes with the comparison deleted, which a drill proved.
//
// This introduces REAL drift, of exactly the kind that happens: a posted journal line against a
// partner with no document behind it. In production that is an invoice posted to the wrong
// partner, a payment allocated across companies, or a rule edited after the fact — and it is
// invisible to every other check, because both sides are internally consistent. Only comparing
// them finds it.
func TestTheVerifierFindsDriftBetweenDocumentsAndTheLedger(t *testing.T) {
	app, ctx, companyID := traded(t)

	created, err := app.Partner.CreatePartner(ctx, partner.NewPartnerInput{
		CompanyID: companyID, Code: "DRIFT", Name: "Drifting Trading", IsCustomer: true,
	})
	if err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}

	account, err := app.Accounting.AccountByCode(ctx, companyID, "1200")
	if err != nil {
		t.Fatalf("AccountByCode: %v", err)
	}
	var periodID string
	if err = app.DB.Reader(ctx).QueryRowContext(ctx, `
		SELECT p.id FROM fiscal_periods p
		  JOIN fiscal_years y ON y.id = p.fiscal_year_id
		 WHERE y.company_id = ? LIMIT 1`, string(companyID)).Scan(&periodID); err != nil {
		t.Fatalf("reading a fiscal period: %v", err)
	}

	// A POSTED receivable with no invoice behind it.
	entryID, _ := id.New()
	lineID, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO journal_entries (
			id, company_id, entry_number, entry_date, fiscal_period_id, status, source_module,
			posted_at, created_at, updated_at, row_version
		) VALUES (?, ?, 'DRIFT-1', '2026-10-01', ?, 'posted', 'test', ?, ?, ?, 1)`,
		string(entryID), string(companyID), periodID, now, now, now); err != nil {
		t.Fatalf("inserting a posted entry: %v", err)
	}
	if _, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO journal_lines (
			id, journal_entry_id, line_number, account_id, debit_minor, credit_minor,
			currency_code, exchange_rate_micro, partner_id, created_at
		) VALUES (?, ?, 1, ?, 750000, 0, 'SYP', 1000000, ?, ?)`,
		string(lineID), string(entryID), string(account.ID),
		string(created.ID), now); err != nil {
		t.Fatalf("inserting a posted line: %v", err)
	}

	discrepancies, err := app.Partner.VerifyBalances(ctx, companyID)
	if err != nil {
		t.Fatalf("VerifyBalances: %v", err)
	}

	var found *partner.BalanceDiscrepancy
	for i := range discrepancies {
		if discrepancies[i].PartnerID == created.ID && discrepancies[i].Side == "receivable" {
			found = &discrepancies[i]
		}
	}
	if found == nil {
		t.Fatalf("the verifier found no drift where the ledger says 7,500 and no document "+
			"does: %+v", discrepancies)
	}
	if found.DocumentsMinor != 0 {
		t.Errorf("documents = %d, want 0", found.DocumentsMinor)
	}
	if found.LedgerMinor != 750_000 {
		t.Errorf("ledger = %d, want 750000", found.LedgerMinor)
	}
	if found.DifferenceMinor != -750_000 {
		t.Errorf("difference = %d, want -750000", found.DifferenceMinor)
	}

	// And it REPORTS rather than repairs — the discipline 4.3 established for stock. A verifier
	// that silently corrected the ledger would hide the posting bug that caused this.
	after, err := app.Accounting.PartnerControlBalance(ctx, companyID, created.ID, "AR")
	if err != nil {
		t.Fatalf("PartnerControlBalance: %v", err)
	}
	if after != 750_000 {
		t.Errorf("the verifier changed the ledger from 750000 to %d", after)
	}
}
