package partner_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/partner/domain"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc       *partner.Service
	audit     *audit.Service
	store     *database.Store
	companyID id.ID
	ctx       context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Partners reference companies, accounts, tax groups, and currencies, so the fixture runs
	// the same merged schema the application does — a partner-only subset would let a broken
	// foreign key pass here and fail on a real install.
	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		org.NewModule(nil).Migrations(),
		audit.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
		accounting.NewModule(nil).Migrations(),
		tax.NewModule(nil).Migrations(),
		partner.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err = store.Writer(ctx).ExecContext(ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, 'SYP', 'Syrian Pound', 'SYP', 0, ?, ?)`,
		string(identifier), now, now); err != nil {
		t.Fatalf("seeding currency: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	auditSvc := audit.NewService(store, clock.System(), nil)
	if err = audit.NewModule(auditSvc).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	orgSvc := org.NewService(store, clock.System(), bus)
	provisioned, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	svc := partner.NewService(store, partner.Options{Clock: clock.System(), Bus: bus})
	return fixture{
		svc: svc, audit: auditSvc, store: store,
		companyID: provisioned.CompanyID, ctx: ctx,
	}
}

func (f fixture) create(t *testing.T, code, name string, customer, supplier bool) domain.Partner {
	t.Helper()
	created, err := f.svc.CreatePartner(f.ctx, partner.NewPartnerInput{
		CompanyID: f.companyID, Code: code, Name: name,
		IsCustomer: customer, IsSupplier: supplier,
	})
	if err != nil {
		t.Fatalf("CreatePartner(%s): %v", code, err)
	}
	return created
}

func codes(list []domain.Partner) []string {
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = p.Code
	}
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// ── decision 9: one table, both roles ───────────────────────────────────────────

// The workshop that buys steel from a merchant and sells them finished brackets. This is the
// shape that makes one table worth its cost, so it is the first thing tested.
func TestAPartnerCanBeBothCustomerAndSupplier(t *testing.T) {
	f := newFixture(t)

	both := f.create(t, "MERCHANT", "Steel Merchant", true, true)
	if !both.IsCustomer || !both.IsSupplier {
		t.Fatal("a partner could not hold both roles")
	}

	// One identity, not two — so the tax number is recorded once and a statement of account is
	// one query.
	customers, err := f.svc.Partners(f.ctx, f.companyID, partner.Filter{CustomersOnly: true})
	if err != nil {
		t.Fatalf("Partners: %v", err)
	}
	suppliers, err := f.svc.Partners(f.ctx, f.companyID, partner.Filter{SuppliersOnly: true})
	if err != nil {
		t.Fatalf("Partners: %v", err)
	}

	if !contains(codes(customers), "MERCHANT") {
		t.Error("the partner is missing from the customer list")
	}
	if !contains(codes(suppliers), "MERCHANT") {
		t.Error("the partner is missing from the supplier list")
	}
	if customers[0].ID != suppliers[0].ID {
		t.Error("the two lists returned different rows for one partner")
	}
}

func TestTheRoleFilterSeparatesPureCustomersFromPureSuppliers(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)
	f.create(t, "MILL", "Steel Mill", false, true)
	f.create(t, "BOTH", "Both Ways", true, true)

	customers, err := f.svc.Partners(f.ctx, f.companyID, partner.Filter{CustomersOnly: true})
	if err != nil {
		t.Fatalf("Partners: %v", err)
	}
	if got := codes(customers); len(got) != 2 ||
		!contains(got, "SHOP") || !contains(got, "BOTH") {
		t.Errorf("customers = %v, want SHOP and BOTH", got)
	}

	suppliers, err := f.svc.Partners(f.ctx, f.companyID, partner.Filter{SuppliersOnly: true})
	if err != nil {
		t.Fatalf("Partners: %v", err)
	}
	if got := codes(suppliers); len(got) != 2 ||
		!contains(got, "MILL") || !contains(got, "BOTH") {
		t.Errorf("suppliers = %v, want MILL and BOTH", got)
	}
}

// A partner who is neither is a row nothing can ever reference: no document can name them, no
// report can include them. Refused by the domain AND by the schema.
func TestAPartnerMustHaveAtLeastOneRole(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.CreatePartner(f.ctx, partner.NewPartnerInput{
		CompanyID: f.companyID, Code: "NOBODY", Name: "Nobody",
	})
	if err == nil {
		t.Fatal("a partner with no role was created")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoRole {
		t.Errorf("code = %q, want %q", code, domain.CodeNoRole)
	}
}

// And the schema keeps it too, so a bad import cannot produce one.
func TestTheDatabaseRefusesARolelessPartner(t *testing.T) {
	f := newFixture(t)

	_, err := f.store.Writer(f.ctx).ExecContext(f.ctx, `
		INSERT INTO partners (
			id, company_id, code, name, is_customer, is_supplier, partner_type,
			payment_terms_days, credit_limit_minor, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES ('x', ?, 'NOBODY', 'Nobody', 0, 0, 'company', 0, 0, 1, 0, 1, ?, ?)`,
		string(f.companyID), "2026-01-01", "2026-01-01")
	if err == nil {
		t.Fatal("the database accepted a partner with neither role")
	}
}

// ── roles and usage ─────────────────────────────────────────────────────────────

// A supplier who has been billed cannot stop being a supplier: their payable sits in the
// accounts, and clearing the flag would hide them from the supplier list while the balance
// remains — a payable that becomes invisible without becoming settled.
func TestARoleThatHasBeenUsedCannotBeRemoved(t *testing.T) {
	f := newFixture(t)
	both := f.create(t, "MERCHANT", "Steel Merchant", true, true)

	// Phase 6 does this on the first purchase.
	if err := f.svc.MarkHistory(f.ctx, both.ID, false, true); err != nil {
		t.Fatalf("MarkHistory: %v", err)
	}

	_, err := f.svc.SetRoles(f.ctx, f.companyID, "MERCHANT", true, false)
	if err == nil {
		t.Fatal("a supplier who has been billed stopped being a supplier")
	}
	if code := errs.CodeOf(err); code != domain.CodeRoleInUse {
		t.Errorf("code = %q, want %q", code, domain.CodeRoleInUse)
	}

	// The refusal left storage alone.
	after, err := f.svc.PartnerByCode(f.ctx, f.companyID, "MERCHANT")
	if err != nil {
		t.Fatalf("PartnerByCode: %v", err)
	}
	if !after.IsSupplier {
		t.Error("a refused role change still took effect")
	}
}

// The other half, and the reason usage is tracked PER ROLE rather than as one flag: the same
// partner may stop being a customer, because nothing was ever sold to them.
func TestAnUnusedRoleCanStillBeRemovedFromAPartnerWithHistory(t *testing.T) {
	f := newFixture(t)
	both := f.create(t, "MERCHANT", "Steel Merchant", true, true)

	if err := f.svc.MarkHistory(f.ctx, both.ID, false, true); err != nil {
		t.Fatalf("MarkHistory: %v", err)
	}

	updated, err := f.svc.SetRoles(f.ctx, f.companyID, "MERCHANT", false, true)
	if err != nil {
		t.Fatalf("removing an unused role: %v", err)
	}
	if updated.IsCustomer {
		t.Error("the unused customer role was not removed")
	}
	if !updated.IsSupplier {
		t.Error("the used supplier role was lost")
	}
}

func TestARoleCanAlwaysBeAdded(t *testing.T) {
	f := newFixture(t)
	customer := f.create(t, "SHOP", "Corner Shop", true, false)
	if err := f.svc.MarkHistory(f.ctx, customer.ID, true, false); err != nil {
		t.Fatalf("MarkHistory: %v", err)
	}

	// A customer who starts supplying is ordinary, and there is nothing to contradict.
	updated, err := f.svc.SetRoles(f.ctx, f.companyID, "SHOP", true, true)
	if err != nil {
		t.Fatalf("adding a role: %v", err)
	}
	if !updated.IsSupplier {
		t.Error("the new role was not added")
	}
}

func TestSetRolesStillRefusesLeavingAPartnerWithNone(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)

	if _, err := f.svc.SetRoles(f.ctx, f.companyID, "SHOP", false, false); err == nil {
		t.Fatal("a partner was left with no role")
	}
}

// ── history ─────────────────────────────────────────────────────────────────────

func TestAPartnerWithHistoryIsDeactivatedNotDeleted(t *testing.T) {
	f := newFixture(t)
	created := f.create(t, "SHOP", "Corner Shop", true, false)

	// Before any document, deletion is ordinary.
	f.create(t, "TEMP", "Mistyped", true, false)
	if err := f.svc.DeletePartner(f.ctx, f.companyID, "TEMP"); err != nil {
		t.Fatalf("deleting an unused partner: %v", err)
	}

	if err := f.svc.MarkHistory(f.ctx, created.ID, true, false); err != nil {
		t.Fatalf("MarkHistory: %v", err)
	}

	err := f.svc.DeletePartner(f.ctx, f.companyID, "SHOP")
	if err == nil {
		t.Fatal("a partner that appears on documents was deleted")
	}
	if code := errs.CodeOf(err); code != domain.CodePartnerHistory {
		t.Errorf("code = %q, want %q", code, domain.CodePartnerHistory)
	}

	// Deactivation is always available, and it leaves every document able to reprint.
	if err = f.svc.Deactivate(f.ctx, f.companyID, "SHOP"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	retired, err := f.svc.PartnerByCode(f.ctx, f.companyID, "SHOP")
	if err != nil {
		t.Fatalf("PartnerByCode: %v", err)
	}
	if retired.IsActive {
		t.Error("the partner was not retired")
	}
	if !retired.HasHistory {
		t.Error("retiring erased the fact that the partner has history")
	}
}

// ── credit ──────────────────────────────────────────────────────────────────────

// Zero means NO LIMIT, not "no credit". The two readings differ by every sale the business
// makes, and a limit of zero refusing everything is never what leaving the field alone meant.
func TestAZeroCreditLimitMeansNoLimit(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)

	shop, err := f.svc.PartnerByCode(f.ctx, f.companyID, "SHOP")
	if err != nil {
		t.Fatalf("PartnerByCode: %v", err)
	}
	if shop.CreditLimitMinor != 0 {
		t.Fatalf("a new partner has a limit of %d, want 0", shop.CreditLimitMinor)
	}
	if err = f.svc.CheckCredit(f.ctx, f.companyID, "SHOP", 0, 999_999_999); err != nil {
		t.Errorf("a partner with no limit was refused credit: %v", err)
	}
}

func TestALimitIsEnforcedAgainstTheOutstandingBalance(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)

	if err := f.svc.SetCreditLimit(f.ctx, f.companyID, "SHOP", 100_000); err != nil {
		t.Fatalf("SetCreditLimit: %v", err)
	}

	// Within the limit.
	if err := f.svc.CheckCredit(f.ctx, f.companyID, "SHOP", 60_000, 40_000); err != nil {
		t.Errorf("a sale exactly at the limit was refused: %v", err)
	}
	// One minor unit past it.
	err := f.svc.CheckCredit(f.ctx, f.companyID, "SHOP", 60_000, 40_001)
	if err == nil {
		t.Fatal("a sale past the credit limit was allowed")
	}
	if code := errs.CodeOf(err); code != domain.CodeCreditExceeded {
		t.Errorf("code = %q, want %q", code, domain.CodeCreditExceeded)
	}
}

// A credit limit on a pure supplier is a field nothing will ever read; storing it silently
// would let someone believe a control exists that does not.
func TestACreditLimitOnAPureSupplierIsRefused(t *testing.T) {
	f := newFixture(t)
	f.create(t, "MILL", "Steel Mill", false, true)

	if err := f.svc.SetCreditLimit(f.ctx, f.companyID, "MILL", 100_000); err == nil {
		t.Fatal("a credit limit was set on a partner who is not a customer")
	} else if code := errs.CodeOf(err); code != partner.CodeNotACustomer {
		t.Errorf("code = %q, want %q", code, partner.CodeNotACustomer)
	}
}

func TestANegativeCreditLimitIsRefused(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)

	if err := f.svc.SetCreditLimit(f.ctx, f.companyID, "SHOP", -1); err == nil {
		t.Fatal("a negative credit limit was accepted")
	}
}

// ── addresses and contacts ──────────────────────────────────────────────────────

func TestAPartnerCanHaveSeveralAddresses(t *testing.T) {
	f := newFixture(t)
	created := f.create(t, "CHAIN", "Retail Chain", true, false)

	for _, in := range []partner.NewAddressInput{
		{
			CompanyID: f.companyID, PartnerCode: "CHAIN", Label: "Head office",
			Type: domain.AddressBilling, Line1: "1 Main Street", IsDefault: true,
		},
		{
			CompanyID: f.companyID, PartnerCode: "CHAIN", Label: "Branch north",
			Type: domain.AddressShipping, Line1: "2 North Road",
		},
		{
			CompanyID: f.companyID, PartnerCode: "CHAIN", Label: "Branch south",
			Type: domain.AddressShipping, Line1: "3 South Road",
		},
	} {
		if _, err := f.svc.AddAddress(f.ctx, in); err != nil {
			t.Fatalf("AddAddress(%s): %v", in.Label, err)
		}
	}

	addresses, err := f.svc.Addresses(f.ctx, created.ID)
	if err != nil {
		t.Fatalf("Addresses: %v", err)
	}
	if len(addresses) != 3 {
		t.Fatalf("%d addresses, want 3", len(addresses))
	}
	// The default comes first, so a document that needs one address takes the right one.
	if !addresses[0].IsDefault || addresses[0].Label != "Head office" {
		t.Errorf("first address = %+v, want the default", addresses[0])
	}
}

// The same partial-index shape the reference unit and the default variant use.
func TestOnlyOneDefaultAddressPerPurpose(t *testing.T) {
	f := newFixture(t)
	f.create(t, "CHAIN", "Retail Chain", true, false)

	first := partner.NewAddressInput{
		CompanyID: f.companyID, PartnerCode: "CHAIN", Label: "A",
		Type: domain.AddressShipping, Line1: "1 Main Street", IsDefault: true,
	}
	if _, err := f.svc.AddAddress(f.ctx, first); err != nil {
		t.Fatalf("AddAddress: %v", err)
	}

	second := first
	second.Label = "B"
	second.Line1 = "2 Other Street"
	if _, err := f.svc.AddAddress(f.ctx, second); err == nil {
		t.Fatal("a second default shipping address was accepted")
	}

	// A default BILLING address is a different purpose and is perfectly fine.
	billing := first
	billing.Label = "C"
	billing.Type = domain.AddressBilling
	billing.Line1 = "3 Billing Street"
	if _, err := f.svc.AddAddress(f.ctx, billing); err != nil {
		t.Errorf("a default billing address was refused: %v", err)
	}
}

func TestAnAddressNeedsALabelAndALine(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)

	// Three unlabelled addresses give a delivery driver nothing to choose between.
	if _, err := f.svc.AddAddress(f.ctx, partner.NewAddressInput{
		CompanyID: f.companyID, PartnerCode: "SHOP", Line1: "1 Main Street",
	}); err == nil {
		t.Error("an unlabelled address was accepted")
	}
	if _, err := f.svc.AddAddress(f.ctx, partner.NewAddressInput{
		CompanyID: f.companyID, PartnerCode: "SHOP", Label: "Office",
	}); err == nil {
		t.Error("an address with no lines was accepted")
	}
}

func TestAPartnerCanHaveContactsWithOnePrimary(t *testing.T) {
	f := newFixture(t)
	created := f.create(t, "CHAIN", "Retail Chain", true, false)

	if _, err := f.svc.AddContact(f.ctx, partner.NewContactInput{
		CompanyID: f.companyID, PartnerCode: "CHAIN", Name: "Layla", Role: "Buyer",
		IsPrimary: true,
	}); err != nil {
		t.Fatalf("AddContact: %v", err)
	}
	if _, err := f.svc.AddContact(f.ctx, partner.NewContactInput{
		CompanyID: f.companyID, PartnerCode: "CHAIN", Name: "Omar", Role: "Accounts",
	}); err != nil {
		t.Fatalf("AddContact: %v", err)
	}

	contacts, err := f.svc.Contacts(f.ctx, created.ID)
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	if len(contacts) != 2 || !contacts[0].IsPrimary || contacts[0].Name != "Layla" {
		t.Errorf("contacts = %+v, want the primary first", contacts)
	}

	// A second primary would leave a document with two people to address it to.
	if _, err = f.svc.AddContact(f.ctx, partner.NewContactInput{
		CompanyID: f.companyID, PartnerCode: "CHAIN", Name: "Sara", IsPrimary: true,
	}); err == nil {
		t.Error("a second primary contact was accepted")
	}
}

// ── searching ───────────────────────────────────────────────────────────────────

// Searching by tax number matters at a counter: the customer hands over a card with a number
// on it and nothing else the operator can type.
func TestPartnersCanBeSearchedByNameCodeOrTaxNumber(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreatePartner(f.ctx, partner.NewPartnerInput{
		CompanyID: f.companyID, Code: "SHOP", Name: "Corner Shop",
		IsCustomer: true, TaxNumber: "TAX-9911",
	}); err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}
	f.create(t, "MILL", "Steel Mill", false, true)

	for _, search := range []string{"Corner", "SHOP", "9911"} {
		found, err := f.svc.Partners(f.ctx, f.companyID, partner.Filter{Search: search})
		if err != nil {
			t.Fatalf("Partners(%s): %v", search, err)
		}
		if got := codes(found); len(got) != 1 || got[0] != "SHOP" {
			t.Errorf("searching %q found %v, want [SHOP]", search, got)
		}
	}
}

// ── audit ───────────────────────────────────────────────────────────────────────

func TestPartnerChangesAreAudited(t *testing.T) {
	f := newFixture(t)
	f.create(t, "SHOP", "Corner Shop", true, false)
	if err := f.svc.SetCreditLimit(f.ctx, f.companyID, "SHOP", 50_000); err != nil {
		t.Fatalf("SetCreditLimit: %v", err)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: partner.EntityPartner})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d entries, want a create and an update: %+v", len(entries), entries)
	}
}

// A refused change records nothing: the audit trail says what happened, and a rejected request
// did not happen. The subscriber runs inside the transaction, so the rollback takes it with it.
func TestARefusedChangeIsNotAudited(t *testing.T) {
	f := newFixture(t)
	created := f.create(t, "MERCHANT", "Steel Merchant", true, true)
	if err := f.svc.MarkHistory(f.ctx, created.ID, false, true); err != nil {
		t.Fatalf("MarkHistory: %v", err)
	}

	before, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: partner.EntityPartner})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}

	if _, err = f.svc.SetRoles(f.ctx, f.companyID, "MERCHANT", true, false); err == nil {
		t.Fatal("the invalid role change was accepted")
	}

	after, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: partner.EntityPartner})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("%d entries after a refused change, want %d", len(after), len(before))
	}
}
