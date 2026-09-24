// Package domain is the notification engine's rules (the owner's request, 2026-09-23): what deserves the shop's
// attention, computed from facts the other modules own. No I/O, no clock, no formatting.
//
// # One engine, not five banners
//
// Before this, each warning found its own way onto the screen — a banner here, a line on a report there — and the
// owner asked for one place: a toast when something happens, a bell that counts what is unread, and a centre that
// lists it all. Every notification is built here, from one set of rules, so the bell, the toast and the centre cannot
// disagree about what is true.
//
// # Keys and fingerprints
//
// A notification's Key is stable while its condition lasts ("low_stock:<product>"). Its Fingerprint changes when the
// condition changes materially — a new exchange rate, a capital shift crossing another five points. The screen marks
// a key+fingerprint read; a notification that gets worse is unread again, and one that is merely still true is not
// shouted about twice.
package domain

import (
	"math/big"
	"sort"
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Kind is what a notification is about.
type Kind string

// The kinds.
const (
	KindLowStock    Kind = "low_stock"
	KindStalePrices Kind = "stale_prices"
	KindCapital     Kind = "capital"
	KindBackupNone  Kind = "backup_none"
	KindBackupOld   Kind = "backup_old"
)

// Severity is how the screen draws it.
type Severity string

// The severities.
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

// Notification is one thing the shop should know.
type Notification struct {
	Key         string
	Fingerprint string
	Kind        Kind
	Severity    Severity
	// Owner is true when the notification discloses an owner's figure — a cost, the stock's value, the shop's worth —
	// and so is held back outside owner mode when the shop has PIN protection on.
	Owner bool
	// ProductID is set for a notification about one product.
	ProductID id.ID
}

// ── Low stock ─────────────────────────────────────────────────────────────────────────────────────────────────────

// Product is what the engine needs to know about one product.
type Product struct {
	ID           id.ID
	NameAR       string
	NameEN       string
	UnitCode     string
	UnitDecimals int
	Active       bool
	OpenPrice    bool
	// ReorderMicro and HasReorder are the level the shop set; a product with none is never low.
	ReorderMicro int64
	HasReorder   bool
}

// Low is one product at or below the level the shop set.
type Low struct {
	Product     Product
	OnHandMicro int64
}

// LowStock is every active, counted product at or below its reorder level, lowest cover first. A product with no level
// is never listed: "low" is a comparison, and only the shop knows the second number.
func LowStock(products []Product, onHand map[id.ID]int64) []Low {
	out := []Low{}
	for _, p := range products {
		if !p.Active || p.OpenPrice || !p.HasReorder || p.ReorderMicro <= 0 {
			continue
		}
		if have := onHand[p.ID]; have <= p.ReorderMicro {
			out = append(out, Low{Product: p, OnHandMicro: have})
		}
	}
	// Emptiest first, relative to what the shop wanted to keep: two left of a level of twenty is worse than two of four.
	sort.SliceStable(out, func(i, j int) bool {
		a := big.NewInt(out[i].OnHandMicro)
		a.Mul(a, big.NewInt(out[j].Product.ReorderMicro))
		b := big.NewInt(out[j].OnHandMicro)
		b.Mul(b, big.NewInt(out[i].Product.ReorderMicro))
		if c := a.Cmp(b); c != 0 {
			return c < 0
		}
		return out[i].Product.NameAR < out[j].Product.NameAR
	})
	return out
}

// ── Stale prices, and selling at a loss ──────────────────────────────────────────────────────────────────────────

// Stale is a product whose price the exchange rate has left behind, with the price the catalogue proposes for it.
type Stale struct {
	ProductID     id.ID
	NameAR        string
	NameEN        string
	Currency      string
	PriceMicro    int64
	ShiftMicro    int64
	ProposedMicro int64
	// CostKnown, ReplacementMicro and Loss say whether the shop is selling below what the product now costs to replace:
	// its average cost in dollars at today's rate, in the product's own currency. Owner-only, because they are costs.
	CostKnown        bool
	ReplacementMicro int64
	Loss             bool
}

// Replacement is what one unit costs to replace today, in the local currency at 10⁻⁶: the dollar average cost at the
// rate in force. big.Int, because micros of an old-pound price times nanos of a rate is ~10²⁶.
func Replacement(avgCostUSDMicro, rateNano int64) int64 {
	if avgCostUSDMicro <= 0 || rateNano <= 0 {
		return 0
	}
	v := new(big.Int).Mul(big.NewInt(avgCostUSDMicro), big.NewInt(rateNano))
	v.Quo(v, big.NewInt(1_000_000_000))
	if !v.IsInt64() {
		return 0
	}
	return v.Int64()
}

// ── What the shop is worth in dollars ────────────────────────────────────────────────────────────────────────────

// Snapshot is what the shop was worth on one business day, each part in its own currency with the rate beside it.
type Snapshot struct {
	BusinessDate   string
	RateNano       int64
	LocalCurrency  string
	StockUSDMinor  int64
	CashUSDMinor   int64
	CashLocalMinor int64
	OwedUSDMinor   int64
	OwedLocalMinor int64
	// PayableUSDMinor and PayableLocalMinor are what the shop owes its suppliers, net (0.10.0): a debt of the shop's is
	// worth taken off what it holds, or a delivery bought on credit would look like the shop growing richer.
	PayableUSDMinor   int64
	PayableLocalMinor int64
}

// NetLocalMinor is the pounds the shop holds net of the pounds it owes: in the drawer and owed to it, less what it owes
// its suppliers. A fall in the pound costs the shop what it holds and saves it what it owes.
func (s Snapshot) NetLocalMinor() int64 {
	return s.CashLocalMinor + s.OwedLocalMinor - s.PayableLocalMinor
}

// LocalToUSD converts local minor units to dollar minor units at a rate, half up.
func LocalToUSD(localMinor, rateNano int64, localDecimals, usdDecimals int) int64 {
	if rateNano <= 0 || localMinor == 0 {
		return 0
	}
	// usd = local ÷ 10^ld ÷ (nano ÷ 10⁹) × 10^ud
	num := new(big.Int).Mul(big.NewInt(localMinor), pow10(9+usdDecimals))
	den := new(big.Int).Mul(big.NewInt(rateNano), pow10(localDecimals))
	return roundDiv(num, den)
}

// TotalUSDMinor is the snapshot in dollars: the stock (already dollars) and each currency's cash and debts, less what is
// owed to suppliers, the local parts at the snapshot's own rate.
func (s Snapshot) TotalUSDMinor(localDecimals, usdDecimals int) int64 {
	return s.StockUSDMinor + s.CashUSDMinor + s.OwedUSDMinor - s.PayableUSDMinor +
		LocalToUSD(s.NetLocalMinor(), s.RateNano, localDecimals, usdDecimals)
}

// Capital compares the shop's worth now with its worth at an earlier snapshot.
type Capital struct {
	Then, Now Snapshot
	// ThenUSD and NowUSD are the two totals in dollar minor units; ChangeMicro the move between them at 10⁻⁶ of a
	// percentage point, signed; RateShiftMicro how far the rate moved over the same days.
	ThenUSD, NowUSD int64
	ChangeMicro     int64
	RateShiftMicro  int64
	// DepreciationUSD is what the pounds the shop held on the earlier day — in the drawer and owed to it, less those it
	// owed its suppliers — lost in dollar value to the rate alone. It separates "the pound fell" from "the shop did
	// badly", which a total cannot. Below nought when the shop owed more pounds than it held: the fall was its gain.
	DepreciationUSD int64
	// Notify is whether the shift is large enough to be worth a notification.
	Notify bool
}

// CapitalThresholdPercent is how far the dollar total must move before it is worth telling the owner. Five points, as
// for a stale price: the total moves with ordinary business — a big delivery, a slow week — and less would notify on
// noise.
const CapitalThresholdPercent = 5

// DepreciationThresholdPercent is lower, and deliberately. What the pounds lost to the rate is value gone with nothing
// happening in the shop — no delivery, no sale, no decision — so there is no noise to allow for. A shop that watched
// 2% of its worth disappear in a month while doing nothing differently should hear about it.
const DepreciationThresholdPercent = 2

// MinHistoryDays and WindowDays are how much history a comparison needs, and how far back it looks. A week is the least
// that says anything; a month is what an owner means by "lately".
const (
	MinHistoryDays = 7
	WindowDays     = 30
)

// CompareCapital measures now against then. Both totals are in dollars at each day's own rate.
func CompareCapital(then, now Snapshot, localDecimals, usdDecimals int) Capital {
	c := Capital{Then: then, Now: now,
		ThenUSD: then.TotalUSDMinor(localDecimals, usdDecimals), NowUSD: now.TotalUSDMinor(localDecimals, usdDecimals)}
	if c.ThenUSD != 0 {
		c.ChangeMicro = roundDiv(new(big.Int).Mul(big.NewInt(c.NowUSD-c.ThenUSD), big.NewInt(100_000_000)), big.NewInt(abs(c.ThenUSD)))
	}
	if then.RateNano > 0 {
		c.RateShiftMicro = roundDiv(new(big.Int).Mul(big.NewInt(now.RateNano-then.RateNano), big.NewInt(100_000_000)), big.NewInt(then.RateNano))
	}
	heldLocal := then.NetLocalMinor()
	c.DepreciationUSD = LocalToUSD(heldLocal, then.RateNano, localDecimals, usdDecimals) -
		LocalToUSD(heldLocal, now.RateNano, localDecimals, usdDecimals)
	threshold := int64(CapitalThresholdPercent) * 1_000_000
	depreciationShare := int64(0)
	if c.ThenUSD > 0 {
		depreciationShare = roundDiv(new(big.Int).Mul(big.NewInt(c.DepreciationUSD), big.NewInt(100_000_000)), big.NewInt(c.ThenUSD))
	}
	c.Notify = abs(c.ChangeMicro) >= threshold || depreciationShare >= int64(DepreciationThresholdPercent)*1_000_000
	return c
}

// ── Backups ──────────────────────────────────────────────────────────────────────────────────────────────────────

// BackupTooOldSeconds is a day and a half: a daily backup missed once is worth saying. Only the shop's OWN backups are
// judged — the outside folder no longer interrupts anyone (D-098.1).
const BackupTooOldSeconds = 36 * 3600

// ── Building the list ────────────────────────────────────────────────────────────────────────────────────────────

// Facts is everything one pass of the engine found.
type Facts struct {
	Low      []Low
	Stale    []Stale
	RateNano int64
	// Capital is nil when there is not yet enough history to compare.
	Capital *Capital
	// BackupFound and BackupAgeSeconds describe the newest of the shop's own backups.
	BackupFound      bool
	BackupAgeSeconds int64
	// Today is the business date. It is the backup notifications' fingerprint: see Notifications.
	Today string
}

// Notifications is the flat list the bell counts and the toasts announce, most urgent first.
func (f Facts) Notifications() []Notification {
	out := []Notification{}
	// An unprotected shop is reminded once a day. The business date is the fingerprint, so each new day without a backup
	// is unread again and announced once — where it used to be a red banner across every screen, the till included,
	// which the owner has asked not to interrupt the counter (0.9.8). Read today, it waits quietly in the centre.
	if !f.BackupFound {
		out = append(out, Notification{Key: string(KindBackupNone), Fingerprint: f.Today, Kind: KindBackupNone, Severity: SeverityWarning})
	} else if f.BackupAgeSeconds > BackupTooOldSeconds {
		out = append(out, Notification{Key: string(KindBackupOld), Fingerprint: f.Today, Kind: KindBackupOld, Severity: SeverityWarning})
	}
	if len(f.Stale) > 0 {
		// A new rate makes the list new: that is the moment the owner asked to be told. What is sold at a loss is NOT in
		// the fingerprint: it is the owner's figure, and a fingerprint that changed on entering owner mode would announce
		// the same list again at every unlock and every lock.
		out = append(out, Notification{Key: string(KindStalePrices), Kind: KindStalePrices, Severity: SeverityWarning,
			Fingerprint: strconv.FormatInt(f.RateNano, 10) + ":" + strconv.Itoa(len(f.Stale))})
	}
	if f.Capital != nil && f.Capital.Notify {
		sev := SeverityInfo
		if f.Capital.ChangeMicro < 0 || f.Capital.DepreciationUSD > 0 {
			sev = SeverityWarning
		}
		// Banded in fives, so a shift drifting from −6% to −7% is the same notification and one crossing −10% is new.
		band := f.Capital.ChangeMicro / (int64(CapitalThresholdPercent) * 1_000_000)
		out = append(out, Notification{Key: string(KindCapital), Kind: KindCapital, Severity: sev, Owner: true,
			Fingerprint: strconv.FormatInt(band, 10)})
	}
	for _, l := range f.Low {
		out = append(out, Notification{Key: string(KindLowStock) + ":" + l.Product.ID.String(), Fingerprint: "low",
			Kind: KindLowStock, Severity: SeverityWarning, ProductID: l.Product.ID})
	}
	return out
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// roundDiv is num ÷ den rounded half away from zero, 0 where it does not fit.
func roundDiv(num, den *big.Int) int64 {
	if den.Sign() == 0 {
		return 0
	}
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if new(big.Int).Abs(new(big.Int).Lsh(r, 1)).Cmp(new(big.Int).Abs(den)) >= 0 {
		if num.Sign()*den.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0
	}
	return q.Int64()
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
