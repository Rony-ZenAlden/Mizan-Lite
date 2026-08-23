package bindings_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/api/bindings"
)

// TestARecordedRateIsTheOneTheEngineUses
//
// # Why this asserts on `inForce` rather than on the list
//
// The rate table is append-only: a correction is a NEW row carrying the same date as the mistake
// it corrects, and the newer row wins. So "the rate for today" is not "the first row" and not
// "the highest date" — it is whatever the resolution order picks, and a screen that guessed would
// eventually disagree with what the till actually charges.
//
// The binding therefore asks the ENGINE which row is in force and marks it. This drill is what
// stops that mark drifting into a re-implementation.
func TestARecordedRateIsTheOneTheEngineUses(t *testing.T) {
	set, _ := signedInAdmin(t)

	first := set.Money.SetRate(bindings.NewRateInput{
		From: "USD", To: "SYP", Rate: "15000", ValidFrom: "2026-08-01",
	})
	if !first.OK {
		t.Fatalf("SetRate: %+v", first.Error)
	}

	/*
	 * A correction: same date, different figure. This is the case the whole append-only design
	 * exists for, and the case a naive "latest row" reading gets wrong.
	 *
	 * The pause is not decoration. Uniqueness is (pair, type, valid_from, created_at) and
	 * timestamps are stored to the MILLISECOND, so two writes inside one tick collide — the
	 * constraint doing exactly its job of blocking an exact double-insert. A person correcting a
	 * rate takes seconds; a test takes microseconds.
	 */
	time.Sleep(2 * time.Millisecond)
	corrected := set.Money.SetRate(bindings.NewRateInput{
		From: "USD", To: "SYP", Rate: "15500", ValidFrom: "2026-08-01",
	})
	if !corrected.OK {
		t.Fatalf("SetRate (correction): %+v", corrected.Error)
	}

	if len(corrected.Data) < 2 {
		t.Fatalf("the history holds %d rows; both the rate and its correction should be there",
			len(corrected.Data))
	}

	// The mistake is still visible — that is what makes "why did this convert at that rate"
	// answerable a year later.
	var sawOriginal bool
	var inForce int
	for _, row := range corrected.Data {
		if strings.HasPrefix(row.Rate, "15000") {
			sawOriginal = true
		}
		if row.InForce {
			inForce++
			if !strings.HasPrefix(row.Rate, "15500") {
				t.Errorf("the rate in force is %q, want the correction", row.Rate)
			}
		}
	}
	if !sawOriginal {
		t.Error("the corrected rate was overwritten rather than superseded")
	}
	// EXACTLY one. Two rows marked in force is a screen telling the user two different things
	// are true, and zero is a screen that cannot say what the till will do.
	if inForce != 1 {
		t.Errorf("%d rows are marked in force, want exactly 1", inForce)
	}
}

// TestARateOfZeroOrLessIsRefused
//
// A zero rate converts every amount to nothing; a negative one flips the sign of every figure on
// a document. Neither is a rate, and both produce documents nobody can explain — so they are
// refused at the boundary rather than stored and discovered later.
func TestARateOfZeroOrLessIsRefused(t *testing.T) {
	set, _ := signedInAdmin(t)

	for _, bad := range []string{"0", "-1", "-15000", "not a number", ""} {
		result := set.Money.SetRate(bindings.NewRateInput{From: "USD", To: "SYP", Rate: bad})
		if result.OK {
			t.Errorf("a rate of %q was accepted", bad)
		}
	}

	// And nothing was written by any of them.
	history := set.Money.Rates("USD", "SYP", "", 0)
	if !history.OK {
		t.Fatalf("Rates: %+v", history.Error)
	}
	if len(history.Data) != 0 {
		t.Errorf("%d rates were stored by submissions that were all refused", len(history.Data))
	}
}

// TestRatesAreReadableByViewersAndWritableOnlyByManagers
//
// Setting a rate changes what every future document converts at. That is a different act from
// reading one, and it carries a different permission.
//
// The UI's own gate is cosmetic (§FE.3); this is the one that matters.
func TestOnlySomebodyWhoManagesTheBooksCanRecordARate(t *testing.T) {
	set, _ := signedInAdmin(t)

	// A cashier: the role a shop actually gives the person at the till, and the one most likely
	// to be signed in when a rate looks wrong.
	if created := set.Identity.CreateUser(bindings.NewUserDTO{
		Username: "sami", DisplayName: "Sami", Password: "a sufficiently long passphrase",
		RoleCode: "cashier",
	}); !created.OK {
		t.Fatalf("CreateUser: %+v", created.Error)
	}
	if out := set.Auth.Logout(); !out.OK {
		t.Fatalf("Logout: %+v", out.Error)
	}
	if login := set.Auth.Login("sami", "a sufficiently long passphrase", false); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}

	write := set.Money.SetRate(bindings.NewRateInput{From: "USD", To: "SYP", Rate: "15000"})
	if write.OK {
		t.Error("a cashier recorded an exchange rate, which reprices every future document")
	}
}

// TestTheRateHistoryIsPerPair
//
// A shop quoting in dollars and euros must not see one pair's rates under the other's heading.
// The filter is trivial and the failure would be catastrophic and quiet: every euro document
// converted at a dollar rate.
func TestTheRateHistoryIsPerPair(t *testing.T) {
	set, _ := signedInAdmin(t)

	if r := set.Money.SetRate(bindings.NewRateInput{
		From: "USD", To: "SYP", Rate: "15000",
	}); !r.OK {
		t.Fatalf("SetRate USD: %+v", r.Error)
	}
	if r := set.Money.SetRate(bindings.NewRateInput{
		From: "EUR", To: "SYP", Rate: "16000",
	}); !r.OK {
		t.Fatalf("SetRate EUR: %+v", r.Error)
	}

	usd := set.Money.Rates("USD", "SYP", "", 0)
	if !usd.OK {
		t.Fatalf("Rates: %+v", usd.Error)
	}
	for _, row := range usd.Data {
		if row.From != "USD" {
			t.Errorf("the USD history contains a %s rate", row.From)
		}
	}
	if len(usd.Data) != 1 {
		t.Errorf("the USD history holds %d rows, want 1", len(usd.Data))
	}
}

// TestALowercasePairFindsTheSameRates
//
// Currency codes are upper-case everywhere in the schema. A caller typing "usd" — a URL, a saved
// filter, a person — must not silently get an empty history that reads as "no rates recorded".
func TestALowercasePairFindsTheSameRates(t *testing.T) {
	set, _ := signedInAdmin(t)

	if r := set.Money.SetRate(bindings.NewRateInput{
		From: "usd", To: "syp", Rate: "15000",
	}); !r.OK {
		t.Fatalf("SetRate: %+v", r.Error)
	}

	lower := set.Money.Rates("usd", "syp", "", 0)
	if !lower.OK {
		t.Fatalf("Rates: %+v", lower.Error)
	}
	if len(lower.Data) != 1 {
		t.Fatalf("a lower-case pair found %d rates, want 1", len(lower.Data))
	}
	if lower.Data[0].From != "USD" || lower.Data[0].To != "SYP" {
		t.Errorf("stored as %s→%s, want USD→SYP", lower.Data[0].From, lower.Data[0].To)
	}
}
