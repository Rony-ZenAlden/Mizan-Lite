// Package domain holds Mizan Lite's stock as values: a product's level, the movements that change it, and the
// costing rules each movement obeys (L2 §3). No I/O, no clock, no identifiers of its own — the service supplies
// those in a Stamp.
//
// # The shape of every act
//
// An act takes the product's current Level and returns the Movement to append and the Level after it. It refuses
// before returning anything, so a refused act has nothing to write. The movement records the level before AND after
// (A-L2.2), which is what makes a receipt reversal exact and lets the verifier walk the chain.
package domain

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Stable error codes. They double as i18n keys.
const (
	CodeQuantityRequired  = "lite.stock.quantity_required"
	CodeQuantityDecimals  = "lite.stock.quantity_decimals"
	CodeQuantityTooLarge  = "lite.stock.quantity_too_large"
	CodeCostDecimals      = "lite.stock.cost_decimals"
	CodeCostTooLarge      = "lite.stock.cost_too_large"
	CodeUnknownCurrency   = "lite.stock.unknown_currency"
	CodeRateRequired      = "lite.stock.rate_required"
	CodeBelowZero         = "lite.stock.below_zero"
	CodeNoCost            = "lite.stock.no_cost"
	CodeNotFirstMovement  = "lite.stock.not_first_movement"
	CodeUnknownReason     = "lite.stock.unknown_reason"
	CodeNoteRequired      = "lite.stock.note_required"
	CodeNoteTooLong       = "lite.stock.note_too_long"
	CodeNotReversible     = "lite.stock.not_reversible"
	CodeNotNewest         = "lite.stock.not_newest"
	CodeCostUnchanged     = "lite.stock.cost_unchanged"
	CodeInactiveProduct   = "lite.stock.inactive_product"
	CodeNotAPackage       = "lite.stock.not_a_package"
	CodeInactiveContent   = "lite.stock.inactive_content"
	CodeMovementNotFound  = "lite.stock.movement_not_found"
	CodeWholePackagesOnly = "lite.stock.whole_packages_only"
)

// codeStale is platform/database's optimistic-concurrency code, reused so a level that moved on reads the same
// whether the service or the database noticed it. The value is the contract; this package may not import the
// database platform.
const codeStale = "database.concurrent_modification"

// Fields named in validation errors, so a dialog can show the message under the right input.
const (
	FieldQuantity = "quantity"
	FieldCost     = "cost"
	FieldCurrency = "currency"
	FieldRate     = "rate"
	FieldReason   = "reason"
	FieldNote     = "note"
	FieldPackages = "packages"
)

// CostCurrency is the currency every cost is held in (DESIGN §4.2).
const CostCurrency = "USD"

// MaxNoteRunes bounds a note, as the schema does.
const MaxNoteRunes = 200

// micro is the fixed-point scale of quantities and unit costs.
const micro = 1_000_000

// costRounding is the one rounding every computed cost uses (L2 §3.3).
const costRounding = round.HalfUp

// Kind is what a movement did.
type Kind string

// The movement kinds, as the schema's CHECK lists them.
const (
	KindOpening         Kind = "opening"
	KindReceipt         Kind = "receipt"
	KindReceiptReversal Kind = "receipt_reversal"
	KindCount           Kind = "count"
	KindAdjustment      Kind = "adjustment"
	KindPackageOut      Kind = "package_out"
	KindContentIn       Kind = "content_in"
	KindCostCorrection  Kind = "cost_correction"
)

// Kinds lists every kind, in the schema's order.
var Kinds = []Kind{KindOpening, KindReceipt, KindReceiptReversal, KindCount, KindAdjustment, KindPackageOut,
	KindContentIn, KindCostCorrection}

// Reason says why a count or adjustment happened (Q-L2.6).
type Reason string

// The reasons. ReasonCount belongs to counts; the others to adjustments.
const (
	ReasonCount   Reason = "count"
	ReasonDamaged Reason = "damaged"
	ReasonExpired Reason = "expired"
	ReasonOwnUse  Reason = "own_use"
	ReasonGift    Reason = "gift"
	ReasonOther   Reason = "other"
)

// AdjustmentReasons are the reasons an adjustment may give, in display order.
var AdjustmentReasons = []Reason{ReasonDamaged, ReasonExpired, ReasonOwnUse, ReasonGift, ReasonOther}

// Currency is a currency a cost may be typed in.
type Currency struct {
	Code     string
	Decimals int
}

// Product is what stock needs to know about a product, supplied by the catalogue through a port.
type Product struct {
	ID id.ID
	// UnitDecimals is how many decimals a quantity of the product may have: 3 for kg and litres, 0 for jars.
	UnitDecimals int
	Active       bool
}

// Package is a package product's link to what it opens into.
type Package struct {
	ContentProductID id.ID
	// ContentQuantityMicro is the content in one package, at 10⁻⁶ of the content product's unit.
	ContentQuantityMicro int64
}

// Level is a product's stock: how much is on hand and what it cost on average.
type Level struct {
	ProductID id.ID
	// OnHandMicro is at 10⁻⁶ of the product's unit.
	OnHandMicro int64
	// AvgCostMicro is at 10⁻⁶ USD per unit.
	AvgCostMicro int64
	// LastMovementID is the newest movement, and LastSeq its place; both zero for a product that never moved.
	LastMovementID id.ID
	LastSeq        int64
	// RowVersion is 0 for a level not yet stored.
	RowVersion int64
}

// Moved reports whether the product has any movement.
func (l Level) Moved() bool { return l.LastSeq > 0 }

// hasCost reports whether there is a cost to enter found stock at. A product with nothing on hand and no average
// has none: it was never received, or everything received was reversed (D-L2.11).
func (l Level) hasCost() bool { return l.AvgCostMicro > 0 || l.OnHandMicro > 0 }

// Entered is what was typed as a receipt's cost: the currency, the unit cost in it, and — for a currency other than
// USD — the rate it was converted at. The zero value means nothing was typed.
type Entered struct {
	Currency        string
	UnitCostMicro   int64
	LocalPerUSDNano int64
}

// Stamp is where and when a movement is written. The service supplies it.
type Stamp struct {
	ID           id.ID
	BusinessDate string
	OccurredAt   time.Time
}

// Movement is one row of the stock ledger.
type Movement struct {
	ID           id.ID
	ProductID    id.ID
	Seq          int64
	BusinessDate string
	OccurredAt   time.Time
	Kind         Kind
	// QuantityMicro is signed: what the movement added to on hand.
	QuantityMicro int64
	// UnitCostMicro is the USD unit cost the movement carried (L2 §3.2's last column).
	UnitCostMicro      int64
	OnHandBeforeMicro  int64
	AvgCostBeforeMicro int64
	OnHandAfterMicro   int64
	AvgCostAfterMicro  int64
	Entered            Entered
	Reason             Reason
	Note               string
	ReversesID         id.ID
	PairID             id.ID
}

// after is the level the movement leaves.
func (m Movement) after(l Level) Level {
	l.OnHandMicro = m.OnHandAfterMicro
	l.AvgCostMicro = m.AvgCostAfterMicro
	l.LastMovementID = m.ID
	l.LastSeq = m.Seq
	return l
}

// movement starts a row on l at stamp: the before-state, and the after-state unchanged until the act sets it.
func movement(l Level, st Stamp, kind Kind) Movement {
	return Movement{
		ID: st.ID, ProductID: l.ProductID, Seq: l.LastSeq + 1, BusinessDate: st.BusinessDate, OccurredAt: st.OccurredAt,
		Kind: kind, OnHandBeforeMicro: l.OnHandMicro, AvgCostBeforeMicro: l.AvgCostMicro,
		OnHandAfterMicro: l.OnHandMicro, AvgCostAfterMicro: l.AvgCostMicro,
	}
}

// Cost is a receipt's cost resolved to USD, with what was typed.
type Cost struct {
	UnitCostMicro int64
	Entered       Entered
}

// Receive books quantity in at cost (L2 §5.1). The average moves by the weighted-average rule; when nothing is on
// hand the receipt's own cost wins (H2).
func Receive(l Level, st Stamp, quantityMicro int64, cost Cost, note string) (Movement, Level, error) {
	return receiveAs(KindReceipt, l, st, quantityMicro, cost, note)
}

// Opening books stock the shop already had when it adopted Lite (L2 §5.2). Only as the product's first movement, so
// it never lands among a month's receipts (D-L2.9).
func Opening(l Level, st Stamp, quantityMicro int64, cost Cost, note string) (Movement, Level, error) {
	if l.Moved() {
		return Movement{}, l, errs.Conflict(CodeNotFirstMovement, "opening stock only as the first movement")
	}
	return receiveAs(KindOpening, l, st, quantityMicro, cost, note)
}

func receiveAs(kind Kind, l Level, st Stamp, quantityMicro int64, cost Cost, note string) (Movement, Level, error) {
	if quantityMicro <= 0 {
		return Movement{}, l, quantityRequired()
	}
	trimmed, err := cleanNote(note, false)
	if err != nil {
		return Movement{}, l, err
	}
	onHandAfter, ok := add(l.OnHandMicro, quantityMicro)
	if !ok {
		return Movement{}, l, quantityTooLarge()
	}
	avg, err := money.WeightedAverageUnit(l.OnHandMicro, l.AvgCostMicro, quantityMicro, cost.UnitCostMicro, costRounding)
	if err != nil {
		return Movement{}, l, costTooLarge(err)
	}
	m := movement(l, st, kind)
	m.QuantityMicro = quantityMicro
	m.UnitCostMicro = cost.UnitCostMicro
	m.OnHandAfterMicro = onHandAfter
	m.AvgCostAfterMicro = avg
	m.Entered = cost.Entered
	m.Note = trimmed
	return m, m.after(l), nil
}

// Count records what is on the shelf (L2 §5.3). The movement is the difference, worked out here at the moment of
// writing; a count that matches records a zero movement. The average never moves (D-L2.4).
func Count(l Level, st Stamp, countedMicro int64, note string) (Movement, Level, error) {
	if countedMicro < 0 {
		return Movement{}, l, quantityRequired()
	}
	if !l.Moved() {
		return Movement{}, l, noCost()
	}
	difference, ok := sub(countedMicro, l.OnHandMicro)
	if !ok {
		return Movement{}, l, quantityTooLarge()
	}
	if difference > 0 && !l.hasCost() {
		return Movement{}, l, noCost()
	}
	trimmed, err := cleanNote(note, false)
	if err != nil {
		return Movement{}, l, err
	}
	m := movement(l, st, KindCount)
	m.QuantityMicro = difference
	m.UnitCostMicro = l.AvgCostMicro
	m.OnHandAfterMicro = countedMicro
	m.Reason = ReasonCount
	m.Note = trimmed
	return m, m.after(l), nil
}

// Adjust adds or removes stock for a reason (L2 §5.4). The average never moves; nothing goes below zero (Q-L2.2);
// "other" needs a note.
func Adjust(l Level, st Stamp, quantityMicro int64, reason Reason, note string) (Movement, Level, error) {
	if quantityMicro == 0 {
		return Movement{}, l, quantityRequired()
	}
	if !knownAdjustmentReason(reason) {
		return Movement{}, l, errs.Validation(CodeUnknownReason, "unknown reason").
			WithField(FieldReason, CodeUnknownReason, "unknown").WithParam("value", string(reason))
	}
	trimmed, err := cleanNote(note, reason == ReasonOther)
	if err != nil {
		return Movement{}, l, err
	}
	if !l.Moved() || (quantityMicro > 0 && !l.hasCost()) {
		return Movement{}, l, noCost()
	}
	onHandAfter, ok := add(l.OnHandMicro, quantityMicro)
	if !ok {
		return Movement{}, l, quantityTooLarge()
	}
	if onHandAfter < 0 {
		return Movement{}, l, belowZero(l)
	}
	m := movement(l, st, KindAdjustment)
	m.QuantityMicro = quantityMicro
	m.UnitCostMicro = l.AvgCostMicro
	m.OnHandAfterMicro = onHandAfter
	m.Reason = reason
	m.Note = trimmed
	return m, m.after(l), nil
}

// Lowers reports whether a movement takes stock out — what needs the owner's PIN (Q-L2.3).
func (m Movement) Lowers() bool { return m.QuantityMicro < 0 }

// ReverseReceipt takes a receipt back out while it is still the product's newest movement (L2 §5.5). The average is
// restored to exactly what it was before the receipt, because the receipt recorded it.
func ReverseReceipt(l Level, receipt Movement, st Stamp, note string) (Movement, Level, error) {
	if receipt.Kind != KindReceipt || receipt.ProductID != l.ProductID {
		return Movement{}, l, errs.Conflict(CodeNotReversible, "only a receipt can be reversed")
	}
	if l.LastMovementID != receipt.ID {
		return Movement{}, l, errs.Conflict(CodeNotNewest, "only the newest movement can be reversed")
	}
	trimmed, err := cleanNote(note, false)
	if err != nil {
		return Movement{}, l, err
	}
	onHandAfter := l.OnHandMicro - receipt.QuantityMicro
	if onHandAfter < 0 {
		return Movement{}, l, belowZero(l)
	}
	m := movement(l, st, KindReceiptReversal)
	m.QuantityMicro = -receipt.QuantityMicro
	m.UnitCostMicro = receipt.UnitCostMicro
	m.OnHandAfterMicro = onHandAfter
	m.AvgCostAfterMicro = receipt.AvgCostBeforeMicro
	m.ReversesID = receipt.ID
	m.Note = trimmed
	return m, m.after(l), nil
}

// CorrectCost sets the average cost to a stated figure, with a required note (L2 §5.6). A movement of quantity zero:
// it moves value, never stock (H3).
func CorrectCost(l Level, st Stamp, avgCostMicro int64, note string) (Movement, Level, error) {
	if avgCostMicro < 0 {
		return Movement{}, l, errs.Validation(CodeCostTooLarge, "a cost cannot be negative").
			WithField(FieldCost, CodeCostTooLarge, "negative")
	}
	if !l.Moved() {
		return Movement{}, l, noCost()
	}
	trimmed, err := cleanNote(note, true)
	if err != nil {
		return Movement{}, l, err
	}
	if avgCostMicro == l.AvgCostMicro {
		return Movement{}, l, errs.Validation(CodeCostUnchanged, "the cost is already this").
			WithField(FieldCost, CodeCostUnchanged, "unchanged")
	}
	m := movement(l, st, KindCostCorrection)
	m.UnitCostMicro = avgCostMicro
	m.AvgCostAfterMicro = avgCostMicro
	m.Note = trimmed
	return m, m.after(l), nil
}

// Opened is the two rows opening packages writes, and the two levels it leaves.
type Opened struct {
	Out, In                 Movement
	PackageLevel, ContentLv Level
}

// OpenPackage opens a whole number of packages into their content (L2 §3.5, §5.7). The package leaves at its
// average; the content enters at that value spread over the content, one rounding, and averages in like a receipt.
func OpenPackage(pkg, content Level, link Package, packagesMicro int64, out, in Stamp, pairID id.ID) (Opened, error) {
	if packagesMicro <= 0 {
		return Opened{}, errs.Validation(CodeQuantityRequired, "open at least one package").
			WithField(FieldPackages, CodeQuantityRequired, "required")
	}
	if packagesMicro%micro != 0 {
		return Opened{}, errs.Validation(CodeWholePackagesOnly, "whole packages only").
			WithField(FieldPackages, CodeWholePackagesOnly, "whole")
	}
	if pkg.OnHandMicro < packagesMicro {
		return Opened{}, belowZero(pkg)
	}
	if link.ContentQuantityMicro <= 0 {
		return Opened{}, errs.Conflict(CodeNotAPackage, "the product opens into nothing")
	}
	contentMicro, ok := mulPositive(packagesMicro/micro, link.ContentQuantityMicro)
	if !ok {
		return Opened{}, quantityTooLarge()
	}
	unitCostIn, err := money.SplitUnitCost(pkg.AvgCostMicro, packagesMicro, contentMicro, costRounding)
	if err != nil {
		return Opened{}, costTooLarge(err)
	}

	o := movement(pkg, out, KindPackageOut)
	o.QuantityMicro = -packagesMicro
	o.UnitCostMicro = pkg.AvgCostMicro
	o.OnHandAfterMicro = pkg.OnHandMicro - packagesMicro
	o.PairID = pairID

	i, contentAfter, err := receiveAs(KindContentIn, content, in, contentMicro, Cost{UnitCostMicro: unitCostIn}, "")
	if err != nil {
		return Opened{}, err
	}
	i.PairID = pairID
	return Opened{Out: o, In: i, PackageLevel: o.after(pkg), ContentLv: contentAfter}, nil
}

func knownAdjustmentReason(r Reason) bool {
	for _, known := range AdjustmentReasons {
		if r == known {
			return true
		}
	}
	return false
}

// cleanNote trims a note and enforces its length, and its presence when required.
func cleanNote(note string, required bool) (string, error) {
	trimmed := strings.TrimSpace(note)
	if required && trimmed == "" {
		return "", errs.Validation(CodeNoteRequired, "a note is required").
			WithField(FieldNote, CodeNoteRequired, "required")
	}
	if utf8.RuneCountInString(trimmed) > MaxNoteRunes {
		return "", errs.Validation(CodeNoteTooLong, "note too long").
			WithField(FieldNote, CodeNoteTooLong, "too long").WithParam("max", strconv.Itoa(MaxNoteRunes))
	}
	return trimmed, nil
}

func quantityRequired() error {
	return errs.Validation(CodeQuantityRequired, "a quantity is required").
		WithField(FieldQuantity, CodeQuantityRequired, "required")
}

func quantityTooLarge() error {
	return errs.Validation(CodeQuantityTooLarge, "quantity too large").
		WithField(FieldQuantity, CodeQuantityTooLarge, "too large")
}

func costTooLarge(err error) error {
	return errs.Wrap(err, errs.CategoryValidation, CodeCostTooLarge, "cost out of range").
		WithField(FieldCost, CodeCostTooLarge, "too large")
}

func noCost() error {
	return errs.Conflict(CodeNoCost, "record an opening stock or a receipt first")
}

// belowZero names what is on hand, so the message can say how much there is to take.
func belowZero(l Level) error {
	return errs.Conflict(CodeBelowZero, "stock cannot go below zero outside the till").
		WithField(FieldQuantity, CodeBelowZero, "below zero").
		WithParam("onHandMicro", strconv.FormatInt(l.OnHandMicro, 10))
}

// ErrStale reports that a level changed since it was read.
func ErrStale() error {
	return errs.Conflict(codeStale, "the stock changed since it was read")
}

// ErrMovementNotFound reports a movement that does not exist.
func ErrMovementNotFound() error {
	return errs.NotFound(CodeMovementNotFound, "no such movement")
}

func add(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}

func sub(a, b int64) (int64, bool) {
	if b == math.MinInt64 {
		return 0, false
	}
	return add(a, -b)
}

// mulPositive multiplies two positive values.
func mulPositive(a, b int64) (int64, bool) {
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}
