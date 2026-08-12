package domain

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for lot and serial tracking.
const (
	CodeLotRequired       = "inventory.lot_required"
	CodeLotNotAllowed     = "inventory.lot_not_allowed"
	CodeSerialRequired    = "inventory.serial_required"
	CodeSerialNotAllowed  = "inventory.serial_not_allowed"
	CodeSerialQuantity    = "inventory.serial_quantity"
	CodeLotExpired        = "inventory.lot_expired"
	CodeLotQuarantined    = "inventory.lot_quarantined"
	CodeInvalidLot        = "inventory.invalid_lot"
	CodeInvalidSerial     = "inventory.invalid_serial"
	CodeSerialUnavailable = "inventory.serial_unavailable"
	CodeNotEnoughLots     = "inventory.not_enough_lots"
)

// Tracking is how finely a product is followed. It mirrors Phase 3's `products.tracking`.
type Tracking string

// The tracking modes.
const (
	TrackNone     Tracking = "none"
	TrackQuantity Tracking = "quantity"
	TrackLot      Tracking = "lot"
	TrackSerial   Tracking = "serial"
)

// Lot is a batch, with the dates that make it matter.
type Lot struct {
	ID             id.ID
	VariantID      id.ID
	Number         string
	ExpiresOn      string
	ManufacturedOn string
	SupplierID     id.ID
	IsQuarantined  bool
	IsActive       bool
}

// NewLot builds a lot, or refuses.
func NewLot(identifier, variantID id.ID, number string) (Lot, error) {
	// Trimmed but NOT upper-cased, unlike most codes here. A lot number is a supplier's
	// identifier printed on a box, and folding its case would make two distinct printed batches
	// collide — the same reasoning a barcode gets in 3.3.
	number = strings.TrimSpace(number)

	if identifier.IsZero() || variantID.IsZero() {
		return Lot{}, errs.Validation(CodeInvalidLot,
			"a lot needs an identity and a variant")
	}
	if number == "" {
		return Lot{}, errs.Validation(CodeInvalidLot, "a lot needs a number").
			WithField("lot_number", CodeInvalidLot, "required")
	}
	return Lot{ID: identifier, VariantID: variantID, Number: number, IsActive: true}, nil
}

// Issuable reports whether stock may be taken from this lot on a given date.
//
// # Expiry is checked at ISSUE, not at receipt
//
// A batch that will expire in a month is perfectly good today, and refusing to receive it would
// stop a delivery for no reason. What must not happen is shipping it after the date — so the
// check belongs where the goods leave.
//
// The date is a parameter rather than read from a clock, because this is a pure rule and because
// a document dated last week must be judged against last week.
func (l Lot) Issuable(on string) error {
	if !l.IsActive {
		return errs.Conflict(CodeLotExpired,
			"that lot is no longer in use").WithParam("lot", l.Number)
	}
	if l.IsQuarantined {
		// Quarantined stock EXISTS and must not be sold: a batch awaiting a quality check, or
		// one under investigation. Distinguished from expired because the remedy differs —
		// quarantine is lifted, expiry never is.
		return errs.Conflict(CodeLotQuarantined,
			"that lot is quarantined and cannot be issued").WithParam("lot", l.Number)
	}
	// An empty expiry means the batch does not expire — a lot number on screws, kept for
	// traceability. Absence is not "expires today".
	if l.ExpiresOn != "" && l.ExpiresOn < on {
		return errs.Conflict(CodeLotExpired,
			"that lot expired").WithParam("lot", l.Number).WithParam("expired_on", l.ExpiresOn)
	}
	return nil
}

// LotStock is one lot's quantity in a warehouse.
type LotStock struct {
	Lot           Lot
	QuantityMicro int64
}

// Pick chooses which lots to draw a quantity from, first-expired-first-out.
//
// # FEFO, not FIFO
//
// Cost layers are consumed in RECEIPT order (§D.2); physical lots are consumed in EXPIRY order.
// They are different orderings for different purposes and must not be conflated: picking by
// receipt order is how a wholesaler ships the batch that expires next week while holding one
// that expires next year, and then writes the second one off.
//
// Lots with no expiry sort LAST. A batch that never expires can wait; one with a date cannot.
//
// # Expired and quarantined lots are skipped, not refused
//
// The caller asked for a quantity, not for a particular batch. Skipping an unusable lot and
// taking from the next is what a picker does. If the usable lots cannot cover the request, THAT
// is the error — and it names the shortfall rather than the first bad lot, because "you have 3
// usable of the 10 you asked for" is actionable and "lot 47 is expired" is not.
func Pick(available []LotStock, quantityMicro int64, on string) ([]LotUse, error) {
	if quantityMicro <= 0 {
		return nil, errs.Validation(CodeNonPositiveQty,
			"a pick must take a positive quantity")
	}

	usable := make([]LotStock, 0, len(available))
	for _, candidate := range available {
		if candidate.QuantityMicro <= 0 {
			continue
		}
		if err := candidate.Lot.Issuable(on); err != nil {
			continue
		}
		usable = append(usable, candidate)
	}

	sort.SliceStable(usable, func(i, j int) bool {
		left, right := usable[i].Lot.ExpiresOn, usable[j].Lot.ExpiresOn
		switch {
		case left == "" && right == "":
			// Neither expires: oldest received first, which the caller's ordering already
			// carries. SliceStable keeps it.
			return false
		case left == "":
			return false // no expiry sorts last
		case right == "":
			return true
		default:
			return left < right
		}
	})

	picks := make([]LotUse, 0, 4)
	remaining := quantityMicro
	for _, candidate := range usable {
		if remaining <= 0 {
			break
		}
		take := candidate.QuantityMicro
		if take > remaining {
			take = remaining
		}
		picks = append(picks, LotUse{LotID: candidate.Lot.ID, QuantityMicro: take})
		remaining -= take
	}

	if remaining > 0 {
		// Named as a shortfall against what is USABLE, which is the number a picker can act on.
		return nil, errs.Conflict(CodeNotEnoughLots,
			"there is not enough usable stock in the lots available").
			WithParam("requested", itoa(quantityMicro)).
			WithParam("available", itoa(quantityMicro-remaining))
	}
	return picks, nil
}

// LotUse is one lot and how much of it a pick takes.
type LotUse struct {
	LotID         id.ID
	QuantityMicro int64
}

// ── the tracking rules ──────────────────────────────────────────────────────────

// Serial is one physical unit.
type Serial struct {
	ID          id.ID
	VariantID   id.ID
	Number      string
	LotID       id.ID
	WarehouseID id.ID
	Status      string
	PartnerID   id.ID
}

// The serial statuses.
const (
	SerialInStock   = "in_stock"
	SerialReserved  = "reserved"
	SerialSold      = "sold"
	SerialReturned  = "returned"
	SerialScrapped  = "scrapped"
	SerialInTransit = "in_transit"
)

// NewSerial builds a serial, or refuses.
func NewSerial(identifier, variantID id.ID, number string) (Serial, error) {
	number = strings.TrimSpace(number)

	if identifier.IsZero() || variantID.IsZero() {
		return Serial{}, errs.Validation(CodeInvalidSerial,
			"a serial needs an identity and a variant")
	}
	if number == "" {
		return Serial{}, errs.Validation(CodeInvalidSerial, "a serial needs a number").
			WithField("serial_number", CodeInvalidSerial, "required")
	}
	return Serial{
		ID: identifier, VariantID: variantID, Number: number, Status: SerialInStock,
	}, nil
}

// RequireIssuable refuses to issue a serial that is not in stock.
//
// A serial that has been sold keeps its row — the warranty claim two years later needs to find
// it — so "does it exist" is not the same question as "can we sell it", and only the second one
// matters here.
func (s Serial) RequireIssuable() error {
	if s.Status != SerialInStock && s.Status != SerialReturned {
		return errs.Conflict(CodeSerialUnavailable,
			"that serial is not available to issue").
			WithParam("serial", s.Number).WithParam("status", s.Status)
	}
	return nil
}

// RequireTracking checks that a movement carries what its product's tracking mode demands.
//
// # Both directions matter
//
// A lot-tracked product moving with no lot is the obvious failure: the batch is untraceable, and
// at a recall nobody can say which customers received it.
//
// The reverse is the one that gets forgotten. A movement that carries a lot for a product which
// is NOT lot-tracked has recorded something nothing will ever read — and worse, it implies to
// whoever reads the row that traceability exists where it does not. A furniture shop must never
// see the concept, which means the data must never carry it either.
func RequireTracking(mode Tracking, m Movement) error {
	hasLot := !m.LotID.IsZero()
	hasSerial := !m.SerialID.IsZero()

	switch mode {
	case TrackLot:
		if !hasLot {
			return errs.Validation(CodeLotRequired,
				"this product is tracked by lot, so its movements need one")
		}
		if hasSerial {
			return errs.Validation(CodeSerialNotAllowed,
				"this product is tracked by lot, not by serial number")
		}
	case TrackSerial:
		if !hasSerial {
			return errs.Validation(CodeSerialRequired,
				"this product is tracked by serial number, so its movements need one")
		}
		// A serial is an IDENTITY, not a quantity: exactly one physical thing moves.
		if m.QuantityMicro != quantityScale {
			return errs.Validation(CodeSerialQuantity,
				"a serialised movement moves exactly one unit").
				WithParam("quantity", itoa(m.QuantityMicro))
		}
	case TrackNone, TrackQuantity:
		if hasLot {
			return errs.Validation(CodeLotNotAllowed,
				"this product is not tracked by lot")
		}
		if hasSerial {
			return errs.Validation(CodeSerialNotAllowed,
				"this product is not tracked by serial number")
		}
	}
	return nil
}
