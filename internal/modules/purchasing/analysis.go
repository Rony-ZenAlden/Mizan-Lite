package purchasing

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/purchasing/infra/sqlite"
)

// Stable codes for the analyses.
const (
	CodeInvalidRange    = "purchasing.invalid_range"
	CodeUnknownGrouping = "purchasing.unknown_grouping"
)

// PermAnalysisView guards what the business spends and with whom.
const PermAnalysisView = "purchasing.analysis.view"

// Grouping is how a period analysis buckets its dates.
type Grouping string

// The groupings.
const (
	ByDay   Grouping = "day"
	ByMonth Grouping = "month"
)

// Spend is one row of a purchase analysis.
//
// # Why this is not sales' Figures
//
// The two shapes are nearly identical, and `module-isolation` is only half the reason they are
// not shared. The other half is that they are not the same thing: a sale has a COST and therefore
// a margin, and a purchase does not — the cost IS the purchase. A shared type would carry a
// `GrossMarginMinor` that is meaningless here, and a reader would have to work out which fields
// apply.
//
// Promotion to a shared kernel type is the answer at the THIRD caller, which is where
// `round.Allocate` and `domain.ValueOf` were both promoted. Two is not a pattern.
type Spend struct {
	Key   string
	Label string
	ID    id.ID

	Documents     int
	QuantityMicro int64
	// NetMinor is after discount and before tax: what the goods actually cost, with recoverable
	// tax excluded because it is reclaimed rather than spent.
	NetMinor int64
}

// PurchaseAnalysis is a whole spend report.
type PurchaseAnalysis struct {
	From  string
	To    string
	Rows  []Spend
	Total Spend
}

// SpendByPeriod reports what was bought per day or per month.
func (s *Service) SpendByPeriod(
	ctx context.Context, companyID id.ID, from, to string, grouping Grouping,
) (PurchaseAnalysis, error) {
	if err := checkAnalysisRange(from, to); err != nil {
		return PurchaseAnalysis{}, err
	}
	if grouping != ByDay && grouping != ByMonth {
		return PurchaseAnalysis{}, errs.Validation(CodeUnknownGrouping,
			"spend can be grouped by day or by month").WithParam("grouping", string(grouping))
	}

	return s.spend(ctx, companyID, from, to, chronological,
		func(line sqlite.PurchasedLine) (string, string, id.ID) {
			date := line.DocumentDate
			if grouping == ByMonth {
				date = date[:7]
			}
			return date, date, id.ID("")
		})
}

// SpendByProduct reports what was bought, per variant.
func (s *Service) SpendByProduct(
	ctx context.Context, companyID id.ID, from, to string,
) (PurchaseAnalysis, error) {
	if err := checkAnalysisRange(from, to); err != nil {
		return PurchaseAnalysis{}, err
	}

	return s.spend(ctx, companyID, from, to, ranked,
		func(line sqlite.PurchasedLine) (string, string, id.ID) {
			// The SNAPSHOTTED name (§9.3), for the same reason 8.2 uses it: a product renamed
			// last month was not bought under its new name.
			label := line.ProductName
			if line.VariantSKU != "" {
				label += " (" + line.VariantSKU + ")"
			}
			return string(line.VariantID), label, line.VariantID
		})
}

// SpendBySupplier reports what was bought from each supplier.
//
// # No walk-in row, unlike the sales equivalent
//
// A bill names its supplier NOT NULL: money is owed to somebody, and 6.5 made that a schema
// constraint rather than a convention. So there is no unattributed bucket here, and a row with no
// partner would be a defect rather than a walk-in.
func (s *Service) SpendBySupplier(
	ctx context.Context, companyID id.ID, from, to string,
) (PurchaseAnalysis, error) {
	if err := checkAnalysisRange(from, to); err != nil {
		return PurchaseAnalysis{}, err
	}

	return s.spend(ctx, companyID, from, to, ranked,
		func(line sqlite.PurchasedLine) (string, string, id.ID) {
			return string(line.PartnerID), line.PartnerName, line.PartnerID
		})
}

// order decides how the rows are arranged.
//
// "Who do we spend the most with" is a ranking; "is this month worse than last" is a sequence.
// The same distinction 8.2 drew, and the same reason: a period report sorted by size answers a
// question nobody asked.
type order int

const (
	ranked order = iota
	chronological
)

func (s *Service) spend(
	ctx context.Context, companyID id.ID, from, to string, arrangement order,
	keyOf func(sqlite.PurchasedLine) (string, string, id.ID),
) (PurchaseAnalysis, error) {
	lines, err := s.repos.PurchasedLinesInRange(ctx, companyID, from, to)
	if err != nil {
		return PurchaseAnalysis{}, err
	}

	var (
		analysis  = PurchaseAnalysis{From: from, To: to}
		byKey     = make(map[string]*Spend, 32)
		documents = make(map[string]map[string]bool, 32)
		keys      = make([]string, 0, 32)
	)

	for _, line := range lines {
		key, label, identifier := keyOf(line)
		row, seen := byKey[key]
		if !seen {
			row = &Spend{Key: key, Label: label, ID: identifier}
			byKey[key] = row
			documents[key] = make(map[string]bool, 8)
			keys = append(keys, key)
		}

		sign := line.Sign()
		row.QuantityMicro += sign * line.QuantityMicro
		row.NetMinor += sign * line.NetMinor
		documents[key][line.DocumentNumber] = true

		analysis.Total.QuantityMicro += sign * line.QuantityMicro
		analysis.Total.NetMinor += sign * line.NetMinor
	}

	analysis.Rows = make([]Spend, 0, len(keys))
	for _, key := range keys {
		row := byKey[key]
		row.Documents = len(documents[key])
		analysis.Rows = append(analysis.Rows, *row)
	}

	sort.SliceStable(analysis.Rows, func(i, j int) bool {
		if arrangement == chronological {
			return analysis.Rows[i].Key < analysis.Rows[j].Key
		}
		if analysis.Rows[i].NetMinor != analysis.Rows[j].NetMinor {
			return analysis.Rows[i].NetMinor > analysis.Rows[j].NetMinor
		}
		return analysis.Rows[i].Key < analysis.Rows[j].Key
	})
	return analysis, nil
}

func checkAnalysisRange(from, to string) error {
	if from == "" || to == "" {
		return errs.Validation(CodeInvalidRange, "an analysis needs both ends of its range")
	}
	if from > to {
		return errs.Validation(CodeInvalidRange, "an analysis's range ends before it starts").
			WithParam("from", from).WithParam("to", to)
	}
	return nil
}
