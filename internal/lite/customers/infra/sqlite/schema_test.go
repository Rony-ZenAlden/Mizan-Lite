package sqlite_test

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// TestDebtEntriesAreNeverUpdatedAndCustomersOnlyByTheirOwnFields reads the store's source (L5 §4).
func TestDebtEntriesAreNeverUpdatedAndCustomersOnlyByTheirOwnFields(t *testing.T) {
	raw, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ToUpper(string(raw))
	if !strings.Contains(src, "INSERT INTO DEBT_ENTRIES") || !strings.Contains(src, "UPDATE CUSTOMERS") {
		t.Fatal("the scan found no insert or update; it is not reading the store")
	}
	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`UPDATE\s+DEBT_ENTRIES`), regexp.MustCompile(`DELETE\s+FROM`), regexp.MustCompile(`REPLACE\s+INTO`),
		regexp.MustCompile(`ON\s+CONFLICT`), regexp.MustCompile(`UPDATE\s+(SALES|STOCK|PRODUCTS|FX)`),
	} {
		if forbidden.MatchString(src) {
			t.Errorf("the store writes what it must not: %s", forbidden)
		}
	}
	if n := len(regexp.MustCompile(`UPDATE\s+`).FindAllString(src, -1)); n != 1 {
		t.Errorf("%d UPDATE statements; the customers' own-fields update is the only one", n)
	}
}

type row map[string]any

func (r row) with(changes row) row {
	out := row{}
	for k, v := range r {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

func insert(db *database.Store, table string, r row) error {
	columns := make([]string, 0, len(r))
	for c := range r {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	args := make([]any, len(columns))
	for i, c := range columns {
		args[i] = r[c]
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	_, err := db.Writer(context.Background()).ExecContext(context.Background(),
		`INSERT INTO `+table+` (`+strings.Join(columns, ", ")+`) VALUES (`+placeholders+`)`, args...)
	return err
}

func uid(t *testing.T) string {
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

// TestACheckRefusesAnImpossibleDebtEntry is L5 §8.1's verification, through the real migration runner: every valid row
// accepted, every impossible one refused by the constraint named — NULL cases included (PROGRESS O7).
func TestACheckRefusesAnImpossibleDebtEntry(t *testing.T) {
	db := litetest.OpenMigrated(t)
	customer := uid(t)
	if err := insert(db, "customers", row{"id": customer, "name": "أبو محمد", "name_key": "ابو محمد", "phone": "0933",
		"created_at": "t", "updated_at": "t"}); err != nil {
		t.Fatal(err)
	}
	sale := insertSale(t, db).String()
	rate := insertRate(t, db).String()
	base := row{"customer_id": customer, "currency": "USD", "seq": 1, "business_date": "2026-09-14", "occurred_at": "t",
		"kind": "opening", "amount_minor": 1_500, "balance_before_minor": 0, "balance_after_minor": 1_500,
		"customer_name_snapshot": "أبو محمد", "created_at": "t"}
	entry := func(changes row) row { return base.with(row{"id": uid(t)}).with(changes) }
	cash := row{"fx_rate_id": rate, "local_per_usd_nano": 15_000_000_000_000, "cash_note_minor": 500, "tendered_currency": "SYP",
		"tendered_minor": 150_000, "change_currency": "SYP", "change_minor": 0}
	payment := func(changes row) row {
		return entry(cash).with(row{"kind": "payment", "amount_minor": -1_000, "balance_before_minor": 1_500, "balance_after_minor": 500}).with(changes)
	}
	tx := func(fn func()) {
		if _, err := db.Writer(context.Background()).ExecContext(context.Background(), "SAVEPOINT t"); err != nil {
			t.Fatal(err)
		}
		fn()
		_, _ = db.Writer(context.Background()).ExecContext(context.Background(), "ROLLBACK TO t")
		_, _ = db.Writer(context.Background()).ExecContext(context.Background(), "RELEASE t")
	}
	first := entry(nil)
	for _, good := range []struct {
		name string
		r    row
	}{
		{"an opening", entry(nil)},
		{"a charge naming its sale", entry(row{"kind": "charge", "sale_id": sale})},
		{"a pounds payment of a dollar debt", payment(nil)},
		{"a write-off with a reason", entry(row{"kind": "write_off", "amount_minor": -1_500, "balance_before_minor": 1_500, "balance_after_minor": 0, "note": "سافر"})},
		{"a refund of a balance below zero", entry(cash).with(row{"kind": "refund", "amount_minor": 300, "balance_before_minor": -300, "balance_after_minor": 0, "note": "أعيد"})},
	} {
		tx(func() {
			if err := insert(db, "debt_entries", good.r); err != nil {
				t.Errorf("%s refused: %v", good.name, err)
			}
		})
	}
	tx(func() {
		if err := insert(db, "debt_entries", first); err != nil {
			t.Fatal(err)
		}
		for _, good := range []row{
			entry(row{"kind": "reversal", "seq": 2, "amount_minor": -1_500, "balance_before_minor": 1_500, "balance_after_minor": 0, "reverses_id": first["id"], "note": "خطأ"}),
		} {
			if err := insert(db, "debt_entries", good); err != nil {
				t.Errorf("a reversal refused: %v", err)
			}
		}
	})
	tx(func() {
		if err := insert(db, "debt_entries", first); err != nil {
			t.Fatal(err)
		}
		if err := insert(db, "debt_entries", entry(row{"kind": "reversal", "seq": 2, "amount_minor": -1_500, "balance_before_minor": 1_000,
			"balance_after_minor": -500, "reverses_id": first["id"], "note": "خطأ"})); err != nil {
			t.Errorf("a reversal below zero refused: %v", err)
		}
	})

	for _, c := range []struct {
		name, constraint string
		r                row
	}{
		{"a broken chain", "ck_debt_chain", entry(row{"balance_after_minor": 1_400})},
		{"a NULL balance (NULL)", "NOT NULL", entry(row{"balance_after_minor": nil})},
		{"an opening that lowers", "ck_debt_sign_matches_kind", entry(row{"amount_minor": -5, "balance_after_minor": -5})},
		{"a payment that raises", "ck_debt_sign_matches_kind", payment(row{"amount_minor": 5, "balance_after_minor": 1_505})},
		{"zero", "amount_minor <> 0", entry(row{"amount_minor": 0, "balance_after_minor": 0})},
		{"a charge without its sale", "ck_debt_charge_names_its_sale", entry(row{"kind": "charge"})},
		{"an opening naming a sale", "ck_debt_charge_names_its_sale", entry(row{"sale_id": sale})},
		{"a reversal of nothing", "ck_debt_reversal_names_its_entry", entry(row{"kind": "reversal", "note": "x"})},
		{"a payment with no tender", "ck_debt_cash_moves_only_on_payment_and_refund", payment(row{"tendered_currency": nil, "tendered_minor": nil})},
		{"a payment with a NULL rate (NULL)", "ck_debt_cash_moves_only_on_payment_and_refund", payment(row{"local_per_usd_nano": nil})},
		{"a payment with NULL change (NULL)", "ck_debt_cash_moves_only_on_payment_and_refund", payment(row{"change_minor": nil})},
		{"a payment with a NULL note (NULL)", "ck_debt_cash_moves_only_on_payment_and_refund", payment(row{"cash_note_minor": nil})},
		{"a payment of zero cash", "ck_debt_cash_moves_only_on_payment_and_refund", payment(row{"tendered_minor": 0})},
		{"an opening carrying cash", "ck_debt_cash_moves_only_on_payment_and_refund", entry(cash)},
		{"a write-off with no reason (NULL)", "ck_debt_owner_acts_have_a_reason", entry(row{"kind": "write_off", "amount_minor": -100, "balance_before_minor": 1_500, "balance_after_minor": 1_400})},
		{"a write-off with a blank reason", "ck_debt_owner_acts_have_a_reason", entry(row{"kind": "write_off", "amount_minor": -100, "balance_before_minor": 1_500, "balance_after_minor": 1_400, "note": "  "})},
		{"a payment below zero", "ck_debt_payment_never_below_zero", payment(row{"amount_minor": -2_000, "balance_after_minor": -500})},
		{"a write-off below zero", "ck_debt_payment_never_below_zero", entry(row{"kind": "write_off", "amount_minor": -2_000, "balance_before_minor": 1_500, "balance_after_minor": -500, "note": "x"})},
		{"a refund of nothing owed back", "ck_debt_refund_only_what_is_owed_back", entry(cash).with(row{"kind": "refund", "amount_minor": 100, "balance_before_minor": 0, "balance_after_minor": 100, "note": "x"})},
		{"a refund past zero", "ck_debt_refund_only_what_is_owed_back", entry(cash).with(row{"kind": "refund", "amount_minor": 500, "balance_before_minor": -300, "balance_after_minor": 200, "note": "x"})},
		{"an unknown customer", "FOREIGN KEY", entry(row{"customer_id": uid(t)})},
		{"an unknown kind", "kind IN", entry(row{"kind": "interest"})},
		{"an unknown currency", "FOREIGN KEY", entry(row{"currency": "EUR"})},
	} {
		tx(func() {
			if err := insert(db, "debt_entries", c.r); err == nil || !strings.Contains(err.Error(), c.constraint) {
				t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
			}
		})
	}

	// Uniqueness: two entries in one place, a second charge for a sale, an entry reversed twice.
	tx(func() {
		if err := insert(db, "debt_entries", first); err != nil {
			t.Fatal(err)
		}
		if err := insert(db, "debt_entries", entry(nil)); err == nil || !strings.Contains(err.Error(), "debt_entries.customer_id, debt_entries.currency, debt_entries.seq") {
			t.Errorf("two entries in one place: %v", err)
		}
		if err := insert(db, "debt_entries", entry(row{"kind": "charge", "seq": 2, "amount_minor": 100, "balance_before_minor": 1_500, "balance_after_minor": 1_600, "sale_id": sale})); err != nil {
			t.Fatal(err)
		}
		if err := insert(db, "debt_entries", entry(row{"kind": "charge", "seq": 3, "amount_minor": 100, "balance_before_minor": 1_600, "balance_after_minor": 1_700, "sale_id": sale})); err == nil || !strings.Contains(err.Error(), "debt_entries.sale_id") {
			t.Errorf("a second charge for one sale: %v", err)
		}
		reversal := entry(row{"kind": "reversal", "seq": 3, "amount_minor": -1_500, "balance_before_minor": 1_600, "balance_after_minor": 100, "reverses_id": first["id"], "note": "x"})
		if err := insert(db, "debt_entries", reversal); err != nil {
			t.Fatal(err)
		}
		if err := insert(db, "debt_entries", reversal.with(row{"id": uid(t), "seq": 4, "balance_before_minor": 100, "balance_after_minor": -1_400})); err == nil || !strings.Contains(err.Error(), "debt_entries.reverses_id") {
			t.Errorf("an entry reversed twice: %v", err)
		}
	})

	for _, c := range []struct {
		name, constraint string
		r                row
	}{
		{"a duplicate name key", "customers.name_key", row{"id": uid(t), "name": "ابو محمد", "name_key": "ابو محمد", "created_at": "t", "updated_at": "t"}},
		{"a blank name", "length(trim(name)) > 0", row{"id": uid(t), "name": "  ", "name_key": "x", "created_at": "t", "updated_at": "t"}},
		{"an empty name key", "length(name_key) > 0", row{"id": uid(t), "name": "x", "name_key": "", "created_at": "t", "updated_at": "t"}},
	} {
		if err := insert(db, "customers", c.r); err == nil || !strings.Contains(err.Error(), c.constraint) {
			t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
		}
	}
}

// TestWhoOwesWhatAt50000EntriesIsFast holds L5 §8.1's plan to a time: 5,000 customers with ten entries each, half written
// off to zero, and the owing list read in well under a second — ten seconds under the race detector, whose instrumentation
// of the driver is what it measures there.
func TestWhoOwesWhatAt50000EntriesIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("50,000 rows")
	}
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	// Generated in SQL, one statement each: customer c; its entries 1–10 of 1,000 cents, the tenth writing the even
	// customers off to zero.
	for _, statement := range []string{
		`WITH RECURSIVE n(c) AS (SELECT 0 UNION ALL SELECT c + 1 FROM n WHERE c < 4999)
		 INSERT INTO customers (id, name, name_key, created_at, updated_at)
		 SELECT printf('00000000-0000-7000-8000-%012d', c), 'زبون ' || c, 'زبون ' || c, 't', 't' FROM n`,
		`WITH RECURSIVE n(c) AS (SELECT 0 UNION ALL SELECT c + 1 FROM n WHERE c < 4999),
		              s(q) AS (SELECT 1 UNION ALL SELECT q + 1 FROM s WHERE q < 10)
		 INSERT INTO debt_entries (id, customer_id, currency, seq, business_date, occurred_at, kind, amount_minor,
		                           balance_before_minor, balance_after_minor, customer_name_snapshot, note, created_at)
		 SELECT printf('00000000-%04d-7000-8000-%012d', q, c), printf('00000000-0000-7000-8000-%012d', c), 'USD', q, '2026-09-14', 't',
		        CASE WHEN q = 10 AND c % 2 = 0 THEN 'write_off' ELSE 'opening' END,
		        CASE WHEN q = 10 AND c % 2 = 0 THEN -9000 ELSE 1000 END,
		        (q - 1) * 1000,
		        CASE WHEN q = 10 AND c % 2 = 0 THEN 0 ELSE q * 1000 END,
		        'x', CASE WHEN q = 10 AND c % 2 = 0 THEN 'x' END, 't'
		   FROM n, s`,
	} {
		if _, err := db.Writer(ctx).ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	var entries int
	if err := db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM debt_entries`).Scan(&entries); err != nil || entries != 50_000 {
		t.Fatalf("%d entries, %v", entries, err)
	}
	store := sqlite.NewStore(db, clock.System())
	start := time.Now()
	owing, err := store.Owing(ctx)
	elapsed := time.Since(start)
	if err != nil || len(owing) != 2_500 {
		t.Fatalf("owing = %d, %v", len(owing), err)
	}
	t.Logf("who owes what over 50,000 entries: %s", elapsed)
	bound := time.Second
	if raceEnabled {
		bound = 10 * time.Second
	}
	if elapsed > bound {
		t.Fatalf("who owes what took %s, over %s", elapsed, bound)
	}
}
