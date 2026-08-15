package sales

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
	"github.com/mizan-erp/mizan/internal/modules/sales/infra/sqlite"
)

// Stable codes for the analyses.
const (
	CodeInvalidRange    = "sales.invalid_range"
	CodeUnknownGrouping = "sales.unknown_grouping"
)

// PermAnalysisView guards every figure in this file.
//
// One permission, not three. The three reports answer the same question cut three ways, and a
// grant that let somebody see revenue per customer but not per product would protect nothing —
// they could add the products up.
const PermAnalysisView = "sales.analysis.view"

// Grouping is how a period analysis buckets its dates.
type Grouping string

// The groupings. A shop asks "how was today", "how was this month", and rarely anything else.
const (
	ByDay   Grouping = "day"
	ByMonth Grouping = "month"
)

// Figures is one row of any sales analysis: what was sold, what it cost, and the difference.
//
// # GrossMarginMinor, never "profit"
//
// Revenue less the cost of the goods. It excludes rent, wages, and every other cost no sale
// knows about, so it is NOT what the shop made — that is the ledger's answer, and accounting's
// `ProfitAndLoss` reports it under a name that says so.
//
// Phase 8's analysis called this the trap in the word "profit": showing one of the two figures
// under that label is how a shopkeeper concludes they are doing well while losing money. Both are
// computed, both are named, and neither is called profit alone.
type Figures struct {
	// Key identifies the row: a date, a variant, or a partner, depending on the analysis.
	Key   string
	Label string
	// ID is the entity the row is about, where there is one. Zero for a period bucket.
	ID id.ID

	// Documents counts the documents contributing, so a caller can tell one large sale from
	// fifty small ones.
	Documents int

	QuantityMicro int64
	// RevenueMinor is NET of discount and BEFORE tax.
	//
	// Tax is not revenue — it is collected on behalf of a government and owed to it — and a
	// margin computed against a tax-inclusive figure would flatter every product by the tax
	// rate. `net_minor` is the line's own after-discount, before-tax figure.
	RevenueMinor     int64
	CostMinor        int64
	GrossMarginMinor int64
}

// MarginPercentMicro is the margin as a proportion of revenue, ×10⁶ (§E's Percent scale).
//
// Zero revenue gives zero rather than an error: a product returned in full has no revenue to be
// a proportion of, and refusing to render the row would hide the return.
func (f Figures) MarginPercentMicro() int64 {
	if f.RevenueMinor == 0 {
		return 0
	}
	return f.GrossMarginMinor * 1_000_000 / f.RevenueMinor
}

// Analysis is a whole report: its rows, and the totals they add to.
type Analysis struct {
	From string
	To   string
	Rows []Figures
	// Total is the sum of the rows. Carried rather than left to the caller, because a caller that
	// summed `MarginPercentMicro` would get an average of percentages, which is not a percentage
	// of anything.
	Total Figures
}

// SalesByPeriod reports what was sold per day or per month.
func (s *Service) SalesByPeriod(
	ctx context.Context, companyID id.ID, from, to string, grouping Grouping,
) (Analysis, error) {
	if err := checkRange(from, to); err != nil {
		return Analysis{}, err
	}
	if grouping != ByDay && grouping != ByMonth {
		return Analysis{}, errs.Validation(CodeUnknownGrouping,
			"sales can be grouped by day or by month").WithParam("grouping", string(grouping))
	}

	return s.analyse(ctx, companyID, from, to, chronological,
		func(line sqlite.SoldLine) (string, string, id.ID) {
			date := line.DocumentDate
			if grouping == ByMonth {
				// An ISO date's first seven characters are its month, which is the reason §E chose
				// the format and not a coincidence being relied on.
				date = date[:7]
			}
			return date, date, id.ID("")
		})
}

// SalesByProduct reports revenue, cost and margin per variant.
//
// Per VARIANT, not per product: a shop selling one shirt in three sizes has three things to
// stock, three costs, and possibly three margins. Aggregating them would hide the size that
// loses money inside the two that do not.
func (s *Service) SalesByProduct(
	ctx context.Context, companyID id.ID, from, to string,
) (Analysis, error) {
	if err := checkRange(from, to); err != nil {
		return Analysis{}, err
	}

	return s.analyse(ctx, companyID, from, to, ranked,
		func(line sqlite.SoldLine) (string, string, id.ID) {
			// The SNAPSHOTTED name, not today's. §9.3's rule applies to a report exactly as it
			// applies to a reprint: a product renamed last month did not sell under its new name.
			label := line.Line.ProductName
			if line.Line.VariantSKU != "" {
				label += " (" + line.Line.VariantSKU + ")"
			}
			return string(line.Line.VariantID), label, line.Line.VariantID
		})
}

// SalesByPartner reports revenue, cost and margin per customer.
func (s *Service) SalesByPartner(
	ctx context.Context, companyID id.ID, from, to string,
) (Analysis, error) {
	if err := checkRange(from, to); err != nil {
		return Analysis{}, err
	}

	return s.analyse(ctx, companyID, from, to, ranked,
		func(line sqlite.SoldLine) (string, string, id.ID) {
			if line.PartnerID.IsZero() {
				// A walk-in sale has no partner, and dropping it would make the rows fail to add up
				// to the period total — which a shopkeeper WILL check.
				return "", "walk-in", id.ID("")
			}
			return string(line.PartnerID), line.PartnerName, line.PartnerID
		})
}

// order decides how an analysis's rows are arranged, which is not a cosmetic choice.
//
// "Which products make money" is a ranking; "is this month better than last" is a sequence. A
// period report sorted by margin puts July before March whenever July did better, which answers
// a question nobody asked and hides the one they did.
type order int

const (
	ranked order = iota
	chronological
)

// analyse is the one aggregation, with the grouping passed in.
//
// # Why one function and not three queries
//
// The three reports differ only in what they key on. Three GROUP BY queries would be three places
// for the credit-note sign to be forgotten, three places for `status = 'posted'` to be left out,
// and three implementations of a cost calculation the domain already owns.
func (s *Service) analyse(
	ctx context.Context, companyID id.ID, from, to string, arrangement order,
	keyOf func(sqlite.SoldLine) (string, string, id.ID),
) (Analysis, error) {
	lines, err := s.repos.SoldLinesInRange(ctx, companyID, from, to)
	if err != nil {
		return Analysis{}, err
	}

	var (
		analysis  = Analysis{From: from, To: to}
		byKey     = make(map[string]*Figures, 32)
		documents = make(map[string]map[string]bool, 32)
		keys      = make([]string, 0, 32)
	)

	for _, line := range lines {
		key, label, identifier := keyOf(line)
		figures, seen := byKey[key]
		if !seen {
			figures = &Figures{Key: key, Label: label, ID: identifier}
			byKey[key] = figures
			documents[key] = make(map[string]bool, 8)
			keys = append(keys, key)
		}

		// The sign, applied ONCE and here. A credit note's lines carry positive quantities, so
		// summing both document types would report a returned sale as two sales.
		sign := line.Sign()
		cost := domain.CostOfLine(line.Line)

		figures.QuantityMicro += sign * line.Line.QuantityStockMicro
		figures.RevenueMinor += sign * line.Line.NetMinor
		figures.CostMinor += sign * cost
		figures.GrossMarginMinor = figures.RevenueMinor - figures.CostMinor

		documents[key][line.DocumentNumber] = true

		analysis.Total.QuantityMicro += sign * line.Line.QuantityStockMicro
		analysis.Total.RevenueMinor += sign * line.Line.NetMinor
		analysis.Total.CostMinor += sign * cost
	}
	analysis.Total.GrossMarginMinor = analysis.Total.RevenueMinor - analysis.Total.CostMinor

	analysis.Rows = make([]Figures, 0, len(keys))
	for _, key := range keys {
		figures := byKey[key]
		figures.Documents = len(documents[key])
		analysis.Rows = append(analysis.Rows, *figures)
	}

	// Ties break on the key in both arrangements, so two rows with identical figures do not swap
	// places between two runs of the same report — which would make a printed copy disagree with
	// the screen it was printed from.
	sort.SliceStable(analysis.Rows, func(i, j int) bool {
		if arrangement == chronological {
			return analysis.Rows[i].Key < analysis.Rows[j].Key
		}
		if analysis.Rows[i].GrossMarginMinor != analysis.Rows[j].GrossMarginMinor {
			return analysis.Rows[i].GrossMarginMinor > analysis.Rows[j].GrossMarginMinor
		}
		return analysis.Rows[i].Key < analysis.Rows[j].Key
	})
	return analysis, nil
}

// checkRange refuses a range that cannot mean anything.
//
// Dates are ISO-8601 throughout, so string comparison IS date comparison (§E).
func checkRange(from, to string) error {
	if from == "" || to == "" {
		return errs.Validation(CodeInvalidRange, "an analysis needs both ends of its range")
	}
	if from > to {
		return errs.Validation(CodeInvalidRange, "an analysis's range ends before it starts").
			WithParam("from", from).WithParam("to", to)
	}
	return nil
}
