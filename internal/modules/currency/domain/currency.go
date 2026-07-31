// Package domain holds the currency module's business rules: the currency and rate-type
// entities, temporal rate resolution, conversion, and redenomination.
//
// Pure Go — stdlib and the kernel only, enforced by the domain-purity architecture rule. It
// knows nothing about SQL, settings, or the event bus; repositories are declared here as
// interfaces and implemented in infra/.
package domain

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Stable error codes. They double as i18n keys and are part of the public contract —
// never rename once shipped.
const (
	CodeUnknownCurrency    = "currency.unknown_currency"
	CodeInvalidCurrency    = "currency.invalid_currency"
	CodeNoRate             = "currency.no_rate"
	CodeInvalidRate        = "currency.invalid_rate"
	CodeUnknownRateType    = "currency.unknown_rate_type"
	CodeRedenominationLoop = "currency.redenomination_loop"
	CodeConversionFailed   = "currency.conversion_failed"
)

// System rate-type codes. Seeded as is_system rows, so code can depend on them existing
// without hardcoding a UUID — the property §CFG.3's is_system column exists for.
const (
	RateTypeOfficial = "official"
	RateTypeMarket   = "market"
	RateTypeCustom   = "custom"
	RateTypeManual   = "manual"
)

// Source values recorded on a resolved rate, so a caller can tell how a number was arrived at.
const (
	SourceIdentity = "identity"
	SourceDirect   = "direct"
	SourceInverse  = "inverse"
	SourcePivot    = "pivot"
)

// maxRedenominationHops bounds the successor walk.
//
// A data-entry mistake that makes A succeed B and B succeed A would otherwise hang the report
// that discovered it. Ten is far beyond any real chain — Turkey and Zimbabwe, the most
// redenominated currencies in modern history, are at three.
const maxRedenominationHops = 10

// Info is a currency as stored: the kernel value type plus the presentation and lifecycle
// fields that only this module cares about.
//
// Other modules receive money.Currency, never this — so adding a column here cannot ripple
// outward.
type Info struct {
	ID             id.ID
	Code           string
	Name           string // base label; translated per locale via the translations table
	Symbol         string
	DecimalPlaces  uint8
	SymbolPosition string
	RoundingMode   round.RoundingMode

	// SucceededBy and RedenominationFactor describe a redenomination (§G.2). The factor is
	// ×10⁹ and fixed forever: an old-unit invoice reprints with its original amount, while
	// reports spanning the change convert through this.
	SucceededBy          string
	RedenominationFactor int64

	IsHistorical bool
	IsSystem     bool
	IsActive     bool
}

// Currency returns the kernel value type for this row.
func (i Info) Currency() (money.Currency, error) {
	c, err := money.NewCurrency(i.Code, i.DecimalPlaces, i.RoundingMode)
	if err != nil {
		return money.Currency{}, errs.Wrap(err, errs.CategoryValidation, CodeInvalidCurrency,
			"stored currency is not valid").WithParam("code", i.Code)
	}
	return c, nil
}

// HasSuccessor reports whether this currency was redenominated into another.
func (i Info) HasSuccessor() bool {
	return i.SucceededBy != "" && i.RedenominationFactor > 0
}

// RateType is a named kind of rate — official, market, custom, manual, or anything a customer
// adds. Rows, not an enum, so a new one needs no migration (§G.1).
type RateType struct {
	ID       id.ID
	Code     string
	Name     string
	IsSystem bool
	IsActive bool
}

// Rate is one stored exchange rate. Temporal and append-only: a correction is a new row with
// a later CreatedAt, never an update.
type Rate struct {
	ID         id.ID
	From       string
	To         string
	RateTypeID id.ID
	Nano       int64 // ×10⁹
	ValidFrom  time.Time
	ValidTo    *time.Time
	Source     string
	BatchID    string
	CreatedAt  time.Time
}

// Money returns the rate as the kernel type.
func (r Rate) Money() money.Rate { return money.RateFromNano(r.Nano) }

// Result is a completed conversion.
//
// It carries provenance, not just a number. §G.3 is explicit that "selling at a three-day-old
// rate during rapid movement is a real loss", so staleness has to be a value the caller
// receives rather than something they could forget to ask for.
type Result struct {
	// Amount is the converted value.
	Amount money.Money
	// Rate is the effective rate used. Store this on the document: §18.1 requires the
	// functional amount and its rate to be recorded, never recomputed, so a report run next
	// year does not silently change because a rate was later corrected.
	Rate money.Rate
	// RateTypeCode is which kind of rate was used.
	RateTypeCode string
	// Path is how the rate was arrived at: identity, direct, inverse, or pivot.
	Path string
	// Source is the stored rate's origin: "manual" or "provider:<key>".
	Source string
	// AsOf is the resolved rate's valid_from.
	AsOf time.Time
	// Stale reports that the rate predates the requested date — it was not set on that day.
	// Age says by how much. A caller decides what threshold matters: a POS may warn above a
	// few hours, a monthly report may not care at all.
	Stale bool
	// Age is how far before the requested date the applied rate was set. Always populated,
	// on every resolution path.
	Age time.Duration
}

// ── repositories, declared inward ───────────────────────────────────────────────

// CurrencyRepository reads the currency table.
type CurrencyRepository interface {
	ByCode(ctx context.Context, code string) (Info, error)
	List(ctx context.Context, includeInactive bool) ([]Info, error)
}

// RateTypeRepository reads the rate-type table.
type RateTypeRepository interface {
	ByCode(ctx context.Context, code string) (RateType, error)
	List(ctx context.Context) ([]RateType, error)
}

// RateRepository reads and appends exchange rates.
type RateRepository interface {
	// Newest returns the most recent rate for the pair and type that is valid on `at`,
	// ordered by valid_from then created_at descending. Found is false when none exists.
	Newest(ctx context.Context, from, to string, rateTypeID id.ID, at time.Time) (Rate, bool, error)
	// LastKnown returns the most recent rate for the pair and type valid at or before `at`,
	// ignoring valid_to. Used for the offline fallback.
	LastKnown(ctx context.Context, from, to string, rateTypeID id.ID, at time.Time) (Rate, bool, error)
	// Append records a new rate. Rates are never updated in place.
	Append(ctx context.Context, r Rate) error
}
