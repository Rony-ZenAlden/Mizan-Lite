package sqlite_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/printing/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/printing/printingtest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

func exec(t *testing.T, db *database.Store, q string, args ...any) {
	t.Helper()
	if _, err := db.Writer(context.Background()).ExecContext(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

// entry writes a customer's opening debt entry for a voucher to number.
func entry(t *testing.T, db *database.Store) id.ID {
	t.Helper()
	customer, _ := id.New()
	e, _ := id.New()
	exec(t, db, `INSERT INTO customers (id, name, name_key, created_at, updated_at) VALUES (?, ?, ?, 't', 't')`, customer.String(), "زبون "+customer.String(), customer.String())
	exec(t, db, `INSERT INTO debt_entries (id, customer_id, currency, seq, business_date, occurred_at, kind, amount_minor, balance_before_minor, balance_after_minor, customer_name_snapshot, created_at)
		VALUES (?, ?, 'USD', 1, '2026-09-15', 't', 'opening', 100, 0, 100, 'x', 't')`, e.String(), customer.String())
	return e
}

func TestSQLiteMeetsTheStoreContract(t *testing.T) {
	printingtest.StoreContract(t, func(t *testing.T) printingtest.Subject {
		db := litetest.OpenMigrated(t)
		return printingtest.Subject{Store: sqlite.NewStore(db), NewEntry: func(t *testing.T) id.ID { return entry(t, db) }}
	})
}

// TestPrintJobsAreInsertOnly reads the store's source.
func TestPrintJobsAreInsertOnly(t *testing.T) {
	raw, _ := os.ReadFile("store.go")
	src := strings.ToUpper(string(raw))
	if !strings.Contains(src, "INSERT INTO PRINT_JOBS") || !strings.Contains(src, "INSERT INTO VOUCHER_NUMBERS") {
		t.Fatal("the scan is not reading the store")
	}
	for _, forbidden := range []*regexp.Regexp{regexp.MustCompile(`UPDATE\s+`), regexp.MustCompile(`DELETE\s+FROM`), regexp.MustCompile(`REPLACE\s+INTO`), regexp.MustCompile(`ON\s+CONFLICT`)} {
		if forbidden.MatchString(src) {
			t.Errorf("the store writes what it must not: %s", forbidden)
		}
	}
}

// TestACheckRefusesAnImpossiblePrintJob: every constraint by name, NULL cases included (PROGRESS O7).
func TestACheckRefusesAnImpossiblePrintJob(t *testing.T) {
	db := litetest.OpenMigrated(t)
	sale, _ := id.New()
	seq := 0
	insert := func(changes map[string]any) error {
		seq++
		row := map[string]any{"id": sale.String() + string(rune('a'+seq)), "seq": seq, "document_kind": "sale", "subject_id": sale.String(), "copy_no": 1,
			"printer_name": "Xprinter", "path": "raw", "outcome": "sent", "error_code": nil, "printed_at": "t"}
		for k, v := range changes {
			row[k] = v
		}
		cols := []string{"id", "seq", "document_kind", "subject_id", "copy_no", "printer_name", "path", "outcome", "error_code", "printed_at"}
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = row[c]
		}
		_, err := db.Writer(context.Background()).ExecContext(context.Background(),
			`INSERT INTO print_jobs (`+strings.Join(cols, ", ")+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
		return err
	}
	for name, good := range map[string]map[string]any{
		"a sent receipt":        nil,
		"a failed copy":         {"copy_no": 2, "outcome": "failed", "error_code": "lite.printers.send_failed"},
		"a test page":           {"document_kind": "test", "subject_id": nil, "path": "driver"},
		"a payment voucher":     {"document_kind": "payment"},
		"a credit sale receipt": {"document_kind": "credit_sale"},
	} {
		if err := insert(good); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	for _, c := range []struct {
		name, constraint string
		row              map[string]any
	}{
		{"an unknown kind", "document_kind IN", map[string]any{"document_kind": "invoice"}},
		{"a receipt of nothing (NULL)", "ck_print_jobs_subject", map[string]any{"subject_id": nil}},
		{"a test page of a sale", "ck_print_jobs_subject", map[string]any{"document_kind": "test"}},
		{"copy zero", "copy_no >= 1", map[string]any{"copy_no": 0}},
		{"no printer", "length(trim(printer_name)) > 0", map[string]any{"printer_name": " "}},
		{"a NULL printer (NULL)", "NOT NULL", map[string]any{"printer_name": nil}},
		{"an unknown path", "path IN", map[string]any{"path": "usb"}},
		{"an unknown outcome", "outcome IN", map[string]any{"outcome": "maybe"}},
		{"a failure without its code (NULL)", "ck_print_jobs_error", map[string]any{"outcome": "failed"}},
		{"a failure with an empty code", "ck_print_jobs_error", map[string]any{"outcome": "failed", "error_code": ""}},
		{"a success with a code", "ck_print_jobs_error", map[string]any{"error_code": "x"}},
		{"place zero", "seq >= 1", map[string]any{"seq": 0}},
	} {
		if err := insert(c.row); err == nil || !strings.Contains(err.Error(), c.constraint) {
			t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
		}
	}
	if err := insert(map[string]any{"seq": 1}); err == nil || !strings.Contains(err.Error(), "print_jobs.seq") {
		t.Errorf("a place used twice: %v", err)
	}
	e := entry(t, db)
	exec(t, db, `INSERT INTO voucher_numbers (entry_id, voucher_no) VALUES (?, 1)`, e.String())
	other := entry(t, db)
	ghost, _ := id.New()
	for _, c := range []struct {
		name, constraint string
		entry            string
		no               any
	}{
		{"a number used twice", "voucher_numbers.voucher_no", other.String(), 1},
		{"an entry numbered twice", "voucher_numbers.entry_id", e.String(), 2},
		{"number zero", "voucher_no >= 1", other.String(), 0},
		{"a NULL number (NULL)", "NOT NULL", other.String(), nil},
		{"an entry that does not exist", "FOREIGN KEY", ghost.String(), 3},
	} {
		_, err := db.Writer(context.Background()).ExecContext(context.Background(), `INSERT INTO voucher_numbers (entry_id, voucher_no) VALUES (?, ?)`, c.entry, c.no)
		if err == nil || !strings.Contains(err.Error(), c.constraint) {
			t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
		}
	}
}
