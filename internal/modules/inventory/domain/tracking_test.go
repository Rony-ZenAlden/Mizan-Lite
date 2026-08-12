package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

const today = "2026-06-15"

func lot(number, expires string, qty int64) domain.LotStock {
	return domain.LotStock{
		Lot: domain.Lot{
			ID: id.ID(number), VariantID: variant, Number: number,
			ExpiresOn: expires, IsActive: true,
		},
		QuantityMicro: qty,
	}
}

func pickedFrom(picks []domain.LotUse) []string {
	out := make([]string, len(picks))
	for i, pick := range picks {
		out[i] = string(pick.LotID)
	}
	return out
}

// ── FEFO ────────────────────────────────────────────────────────────────────────

// Cost layers are consumed in RECEIPT order; physical lots in EXPIRY order. Conflating them is
// how a wholesaler ships the batch that expires next week while holding one that expires next
// year, and then writes the second one off.
func TestLotsArePickedFirstExpiredFirstOut(t *testing.T) {
	// Deliberately given in an order that is neither expiry nor receipt order.
	available := []domain.LotStock{
		lot("LATE", "2027-01-01", 10_000_000),
		lot("SOON", "2026-07-01", 10_000_000),
		lot("MID", "2026-09-01", 10_000_000),
	}

	picks, err := domain.Pick(available, 25_000_000, today)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}

	want := []string{"SOON", "MID", "LATE"}
	got := pickedFrom(picks)
	if len(got) != 3 {
		t.Fatalf("picked from %v, want three lots", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pick %d came from %s, want %s", i, got[i], want[i])
		}
	}
	// The last lot is only partly drawn on.
	if picks[2].QuantityMicro != 5_000_000 {
		t.Errorf("last pick took %d, want 5000000", picks[2].QuantityMicro)
	}
}

// A batch that never expires can wait; one with a date cannot. Sorting the dateless lot first
// would leave the perishable one to rot.
func TestALotWithNoExpirySortsLast(t *testing.T) {
	available := []domain.LotStock{
		lot("FOREVER", "", 10_000_000),
		lot("PERISHABLE", "2026-07-01", 10_000_000),
	}

	picks, err := domain.Pick(available, 5_000_000, today)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if got := pickedFrom(picks); len(got) != 1 || got[0] != "PERISHABLE" {
		t.Errorf("picked %v, want the perishable lot first", got)
	}
}

// ── what a picker skips ─────────────────────────────────────────────────────────

// The caller asked for a quantity, not for a particular batch. Skipping an unusable lot and
// taking from the next is what a picker does.
func TestAnExpiredLotIsSkippedRatherThanRefused(t *testing.T) {
	available := []domain.LotStock{
		lot("EXPIRED", "2026-01-01", 10_000_000),
		lot("GOOD", "2027-01-01", 10_000_000),
	}

	picks, err := domain.Pick(available, 5_000_000, today)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if got := pickedFrom(picks); len(got) != 1 || got[0] != "GOOD" {
		t.Errorf("picked %v, want only the unexpired lot", got)
	}
}

func TestAQuarantinedLotIsSkipped(t *testing.T) {
	held := lot("HELD", "2027-01-01", 10_000_000)
	held.Lot.IsQuarantined = true

	picks, err := domain.Pick(
		[]domain.LotStock{held, lot("GOOD", "2027-06-01", 10_000_000)}, 5_000_000, today)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if got := pickedFrom(picks); len(got) != 1 || got[0] != "GOOD" {
		t.Errorf("picked %v — quarantined stock was shipped", got)
	}
}

// Expired and quarantined are DIFFERENT conditions, because the remedy differs: quarantine is
// lifted, expiry never is.
func TestExpiryAndQuarantineAreDistinctRefusals(t *testing.T) {
	expired := domain.Lot{Number: "E", ExpiresOn: "2026-01-01", IsActive: true}
	if err := expired.Issuable(today); errs.CodeOf(err) != domain.CodeLotExpired {
		t.Errorf("expired lot gave %q, want %q", errs.CodeOf(err), domain.CodeLotExpired)
	}

	held := domain.Lot{Number: "Q", ExpiresOn: "2027-01-01", IsActive: true, IsQuarantined: true}
	if err := held.Issuable(today); errs.CodeOf(err) != domain.CodeLotQuarantined {
		t.Errorf("quarantined lot gave %q, want %q", errs.CodeOf(err), domain.CodeLotQuarantined)
	}
}

// A lot expiring TODAY is still good today. Off by one here means throwing away a day's stock,
// every day.
func TestALotIsGoodOnItsExpiryDate(t *testing.T) {
	expiring := domain.Lot{Number: "E", ExpiresOn: today, IsActive: true}
	if err := expiring.Issuable(today); err != nil {
		t.Errorf("a lot expiring today was refused today: %v", err)
	}
	if err := expiring.Issuable("2026-06-16"); err == nil {
		t.Error("a lot was issued the day after it expired")
	}
}

// An empty expiry means the batch does not expire — a lot number on screws, kept for
// traceability. Absence is not "expires today".
func TestALotWithNoExpiryNeverExpires(t *testing.T) {
	screws := domain.Lot{Number: "S", IsActive: true}
	for _, date := range []string{"1999-01-01", today, "2099-12-31"} {
		if err := screws.Issuable(date); err != nil {
			t.Errorf("a lot with no expiry was refused on %s: %v", date, err)
		}
	}
}

// The shortfall names what is USABLE, which is what a picker can act on — not the first bad lot,
// which is not.
func TestAShortfallNamesTheUsableQuantity(t *testing.T) {
	available := []domain.LotStock{
		lot("EXPIRED", "2026-01-01", 100_000_000),
		lot("GOOD", "2027-01-01", 3_000_000),
	}

	_, err := domain.Pick(available, 10_000_000, today)
	if err == nil {
		t.Fatal("ten units were picked from three usable ones")
	}
	if code := errs.CodeOf(err); code != domain.CodeNotEnoughLots {
		t.Errorf("code = %q, want %q", code, domain.CodeNotEnoughLots)
	}
	typed, _ := errs.AsError(err)
	if typed.Params["available"] != "3000000" {
		t.Errorf("params = %v, want the USABLE quantity, not the total on hand", typed.Params)
	}
}

// ── the tracking rules, in both directions ──────────────────────────────────────

func TestALotTrackedProductCannotMoveWithoutALot(t *testing.T) {
	m := movement(t, "m1", domain.Receipt, 5_000_000)

	if err := domain.RequireTracking(domain.TrackLot, m); err == nil {
		t.Fatal("a lot-tracked product moved with no lot")
	} else if code := errs.CodeOf(err); code != domain.CodeLotRequired {
		t.Errorf("code = %q, want %q", code, domain.CodeLotRequired)
	}

	m.LotID = id.ID("lot-47")
	if err := domain.RequireTracking(domain.TrackLot, m); err != nil {
		t.Errorf("a lot-tracked movement carrying a lot was refused: %v", err)
	}
}

// The direction that gets forgotten. A movement carrying a lot for a product that is NOT
// lot-tracked has recorded something nothing will read — and worse, implies traceability exists
// where it does not. A furniture shop must never see the concept, so the data must not carry it.
func TestAnUntrackedProductCannotMoveWithALot(t *testing.T) {
	m := movement(t, "m1", domain.Receipt, 5_000_000)
	m.LotID = id.ID("lot-47")

	for _, mode := range []domain.Tracking{domain.TrackNone, domain.TrackQuantity} {
		err := domain.RequireTracking(mode, m)
		if err == nil {
			t.Errorf("a %s-tracked product moved with a lot", mode)
			continue
		}
		if code := errs.CodeOf(err); code != domain.CodeLotNotAllowed {
			t.Errorf("code = %q, want %q", code, domain.CodeLotNotAllowed)
		}
	}
}

func TestASerialTrackedProductNeedsASerial(t *testing.T) {
	m := movement(t, "m1", domain.Receipt, 1_000_000)

	if err := domain.RequireTracking(domain.TrackSerial, m); err == nil {
		t.Fatal("a serialised product moved with no serial")
	} else if code := errs.CodeOf(err); code != domain.CodeSerialRequired {
		t.Errorf("code = %q, want %q", code, domain.CodeSerialRequired)
	}

	m.SerialID = id.ID("imei-1")
	if err := domain.RequireTracking(domain.TrackSerial, m); err != nil {
		t.Errorf("a serialised movement carrying a serial was refused: %v", err)
	}
}

// A serial is an IDENTITY, not a quantity. Two of them is two movements.
func TestASerialisedMovementMovesExactlyOneUnit(t *testing.T) {
	m := movement(t, "m1", domain.Receipt, 3_000_000)
	m.SerialID = id.ID("imei-1")

	if err := domain.RequireTracking(domain.TrackSerial, m); err == nil {
		t.Fatal("three units moved under one serial number")
	} else if code := errs.CodeOf(err); code != domain.CodeSerialQuantity {
		t.Errorf("code = %q, want %q", code, domain.CodeSerialQuantity)
	}

	// Half a phone is not a thing either.
	half := movement(t, "m2", domain.Receipt, 500_000)
	half.SerialID = id.ID("imei-1")
	if err := domain.RequireTracking(domain.TrackSerial, half); err == nil {
		t.Error("half a unit moved under a serial number")
	}
}

func TestALotTrackedProductCannotCarryASerial(t *testing.T) {
	m := movement(t, "m1", domain.Receipt, 5_000_000)
	m.LotID = id.ID("lot-47")
	m.SerialID = id.ID("imei-1")

	if err := domain.RequireTracking(domain.TrackLot, m); err == nil {
		t.Fatal("a lot-tracked movement carried a serial number too")
	}
}

// ── serial availability ─────────────────────────────────────────────────────────

// A sold serial keeps its row — the warranty claim two years later needs to find it — so
// "does it exist" is not "can we sell it".
func TestASoldSerialCannotBeIssuedAgain(t *testing.T) {
	sold := domain.Serial{Number: "IMEI-1", Status: domain.SerialSold}

	if err := sold.RequireIssuable(); err == nil {
		t.Fatal("a sold serial was issued again")
	} else if code := errs.CodeOf(err); code != domain.CodeSerialUnavailable {
		t.Errorf("code = %q, want %q", code, domain.CodeSerialUnavailable)
	}

	// A returned one is back in stock and sellable again.
	returned := domain.Serial{Number: "IMEI-2", Status: domain.SerialReturned}
	if err := returned.RequireIssuable(); err != nil {
		t.Errorf("a returned serial could not be resold: %v", err)
	}
}

func TestAScrappedSerialCannotBeIssued(t *testing.T) {
	for _, status := range []string{
		domain.SerialScrapped, domain.SerialInTransit, domain.SerialReserved,
	} {
		serial := domain.Serial{Number: "IMEI-1", Status: status}
		if err := serial.RequireIssuable(); err == nil {
			t.Errorf("a %s serial was issued", status)
		}
	}
}

// ── construction ────────────────────────────────────────────────────────────────

// A lot number is a SUPPLIER's identifier printed on a box. Folding its case would make two
// distinct printed batches collide — the same reasoning a barcode gets in 3.3.
func TestALotNumberIsTrimmedButNotCaseFolded(t *testing.T) {
	built, err := domain.NewLot(id.ID("l1"), variant, "  ab12x  ")
	if err != nil {
		t.Fatalf("NewLot: %v", err)
	}
	if built.Number != "ab12x" {
		t.Errorf("number = %q, want ab12x — trimmed but not upper-cased", built.Number)
	}
}

func TestALotAndASerialBothNeedANumber(t *testing.T) {
	if _, err := domain.NewLot(id.ID("l1"), variant, "   "); err == nil {
		t.Error("a lot with no number was accepted")
	}
	if _, err := domain.NewSerial(id.ID("s1"), variant, "   "); err == nil {
		t.Error("a serial with no number was accepted")
	}
}

func TestANewSerialStartsInStock(t *testing.T) {
	built, err := domain.NewSerial(id.ID("s1"), variant, "IMEI-1")
	if err != nil {
		t.Fatalf("NewSerial: %v", err)
	}
	if built.Status != domain.SerialInStock {
		t.Errorf("status = %q, want in_stock", built.Status)
	}
}
