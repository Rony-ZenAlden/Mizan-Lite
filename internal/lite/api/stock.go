package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	fxdomain "github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// Stock is what is on hand, what it cost, and the acts that move it (L2).
//
// Quantities cross as decimal strings in the unit's decimals ("12.500" kg, "3" jars) and costs as USD decimal strings,
// all formatted by Go: the frontend does no arithmetic on stock or money (DESIGN D9).
type Stock struct{ core *core }

// StockLevelDTO is a product's quantity. It has no cost field, by design (Q-L2.4): TestLevelsCarryNoCost holds it so.
type StockLevelDTO struct {
	ProductID string `json:"productId"`
	OnHand    string `json:"onHand"`
}

// ValuationLineDTO is one product's cost and value, in owner mode.
type ValuationLineDTO struct {
	ProductID   string `json:"productId"`
	OnHand      string `json:"onHand"`
	AverageCost string `json:"averageCost"`
	Value       string `json:"value"`
	// ValueLocal is the line's value in the local currency at the rate in force, rounded once per line (L3 §5.4), or "".
	ValueLocal string `json:"valueLocal"`
}

// ValuationDTO is every product's cost and value, and the total, in USD.
type ValuationDTO struct {
	Lines []ValuationLineDTO `json:"lines"`
	Total string             `json:"total"`
	// TotalLocal is the sum of the lines' local values; LocalCurrency and Rate say what it was converted at. All three are
	// empty when there is no rate (Q-L3.6).
	TotalLocal    string `json:"totalLocal"`
	LocalCurrency string `json:"localCurrency"`
	Rate          string `json:"rate"`
}

// MovementsQueryDTO asks for a product's history.
type MovementsQueryDTO struct {
	ProductID string `json:"productId"`
	Limit     int    `json:"limit"`
}

// MovementDTO is one ledger row. The cost fields are empty outside owner mode — cleared by the service, not hidden by
// the screen.
type MovementDTO struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	BusinessDate string `json:"businessDate"`
	OccurredAt   string `json:"occurredAt"`
	Quantity     string `json:"quantity"`
	OnHandAfter  string `json:"onHandAfter"`
	Reason       string `json:"reason"`
	Note         string `json:"note"`
	ReversesID   string `json:"reversesId"`
	PairID       string `json:"pairId"`

	UnitCost          string `json:"unitCost"`
	AverageCostBefore string `json:"averageCostBefore"`
	AverageCostAfter  string `json:"averageCostAfter"`
	EnteredCurrency   string `json:"enteredCurrency"`
	EnteredUnitCost   string `json:"enteredUnitCost"`
	Rate              string `json:"rate"`
}

// HistoryDTO is a product's newest movements.
type HistoryDTO struct {
	Movements    []MovementDTO `json:"movements"`
	CostsVisible bool          `json:"costsVisible"`
	// ReversibleID is the receipt a reversal may be offered on, or "".
	ReversibleID string `json:"reversibleId"`
}

// ReceiveInput is a delivery or opening stock as typed. CostMode is "total" (the default) or "unit"; Rate is the
// pounds per dollar the shop paid at, for a cost in pounds (Q-L2.1).
type ReceiveInput struct {
	ProductID string `json:"productId"`
	Quantity  string `json:"quantity"`
	CostMode  string `json:"costMode"`
	Cost      string `json:"cost"`
	Currency  string `json:"currency"`
	Rate      string `json:"rate"`
	Note      string `json:"note"`
}

// CountInput is what is on the shelf.
type CountInput struct {
	ProductID string `json:"productId"`
	Counted   string `json:"counted"`
	Note      string `json:"note"`
}

// AdjustInput adds ("in") or writes off ("out") stock for a reason.
type AdjustInput struct {
	ProductID string `json:"productId"`
	Direction string `json:"direction"`
	Quantity  string `json:"quantity"`
	Reason    string `json:"reason"`
	Note      string `json:"note"`
}

// OpenPackageInput opens whole packages.
type OpenPackageInput struct {
	PackageProductID string `json:"packageProductId"`
	Packages         string `json:"packages"`
}

// ReverseReceiptInput takes back the newest receipt.
type ReverseReceiptInput struct {
	MovementID string `json:"movementId"`
	Note       string `json:"note"`
}

// CorrectCostInput sets a product's average cost in USD.
type CorrectCostInput struct {
	ProductID   string `json:"productId"`
	AverageCost string `json:"averageCost"`
	Note        string `json:"note"`
}

// FindingDTO is one thing the verifier found.
type FindingDTO struct {
	Code       string `json:"code"`
	ProductID  string `json:"productId"`
	MovementID string `json:"movementId"`
}

func toLevelDTO(o stock.OnHand) StockLevelDTO {
	return StockLevelDTO{ProductID: o.ProductID.String(), OnHand: domain.FormatScaled(o.OnHandMicro, o.UnitDecimals)}
}

// Levels lists the quantity of every product that has moved.
func (s *Stock) Levels() envelope.Result[[]StockLevelDTO] {
	return call(s.core, "Stock.Levels", func(ctx context.Context, app *bootstrap.App) ([]StockLevelDTO, error) {
		levels, err := app.Stock.Levels(ctx)
		out := make([]StockLevelDTO, 0, len(levels))
		for _, l := range levels {
			out = append(out, toLevelDTO(l))
		}
		return out, err
	})
}

// Valuation is every product's average cost and value. Outside owner mode it returns lite.owner.required.
func (s *Stock) Valuation() envelope.Result[ValuationDTO] {
	return call(s.core, "Stock.Valuation", func(ctx context.Context, app *bootstrap.App) (ValuationDTO, error) {
		v, err := app.Stock.Valuation(ctx)
		if err != nil {
			return ValuationDTO{}, err
		}
		out := ValuationDTO{Lines: make([]ValuationLineDTO, 0, len(v.Lines)), Total: domain.FormatMinor(v.TotalMinor)}
		ref, err := app.Catalog.Reference(ctx)
		if err != nil {
			return ValuationDTO{}, err
		}
		rate, local, usd, hasRate, err := rateView(ctx, app, ref)
		if err != nil {
			return ValuationDTO{}, err
		}
		var totalLocal int64
		for _, l := range v.Lines {
			line := ValuationLineDTO{
				ProductID: l.ProductID.String(), OnHand: domain.FormatScaled(l.OnHandMicro, l.UnitDecimals),
				AverageCost: domain.FormatScaled(l.AvgCostMicro, 2), Value: domain.FormatMinor(l.ValueMinor),
			}
			if hasRate {
				converted, err := rate.LineInLocal(l.AvgCostMicro, l.OnHandMicro, local, usd)
				if err != nil {
					return ValuationDTO{}, err
				}
				line.ValueLocal = converted.Text()
				totalLocal += converted.Minor
			}
			out.Lines = append(out.Lines, line)
		}
		if hasRate {
			out.TotalLocal = fxdomain.FormatMinor(totalLocal, local.Decimals)
			out.LocalCurrency, out.Rate = local.Code, fxdomain.FormatRate(rate.Nano)
		}
		return out, nil
	})
}

// Movements returns a product's newest movements.
func (s *Stock) Movements(q MovementsQueryDTO) envelope.Result[HistoryDTO] {
	return call(s.core, "Stock.Movements", func(ctx context.Context, app *bootstrap.App) (HistoryDTO, error) {
		productID, err := parseProductID(q.ProductID)
		if err != nil {
			return HistoryDTO{}, err
		}
		h, err := app.Stock.History(ctx, productID, q.Limit)
		if err != nil {
			return HistoryDTO{}, err
		}
		decimals, err := currencyDecimals(ctx, app)
		if err != nil {
			return HistoryDTO{}, err
		}
		out := HistoryDTO{Movements: make([]MovementDTO, 0, len(h.Movements)), CostsVisible: h.CostsVisible, ReversibleID: h.ReversibleID.String()}
		for _, m := range h.Movements {
			dto := MovementDTO{
				ID: m.ID.String(), Kind: string(m.Kind), BusinessDate: m.BusinessDate, OccurredAt: clock.Format(m.OccurredAt),
				Quantity: domain.FormatScaled(m.QuantityMicro, h.UnitDecimals), OnHandAfter: domain.FormatScaled(m.OnHandAfterMicro, h.UnitDecimals),
				Reason: string(m.Reason), Note: m.Note, ReversesID: m.ReversesID.String(), PairID: m.PairID.String(),
			}
			if h.CostsVisible {
				dto.UnitCost = domain.FormatScaled(m.UnitCostMicro, 2)
				dto.AverageCostBefore = domain.FormatScaled(m.AvgCostBeforeMicro, 2)
				dto.AverageCostAfter = domain.FormatScaled(m.AvgCostAfterMicro, 2)
				if m.Entered.Currency != "" {
					dto.EnteredCurrency = m.Entered.Currency
					dto.EnteredUnitCost = domain.FormatScaled(m.Entered.UnitCostMicro, decimals[m.Entered.Currency])
				}
				if m.Entered.LocalPerUSDNano != 0 {
					dto.Rate = domain.FormatRate(m.Entered.LocalPerUSDNano)
				}
			}
			out.Movements = append(out.Movements, dto)
		}
		return out, nil
	})
}

func (in ReceiveInput) toService() (stock.ReceiveInput, error) {
	productID, err := parseProductID(in.ProductID)
	return stock.ReceiveInput{
		ProductID: productID, Quantity: in.Quantity, Note: in.Note,
		Cost: domain.CostInput{Mode: domain.CostMode(in.CostMode), Amount: in.Cost, Currency: in.Currency, Rate: in.Rate},
	}, err
}

// Receive books a delivery in.
func (s *Stock) Receive(in ReceiveInput) envelope.Result[StockLevelDTO] {
	return s.act("Stock.Receive", func(ctx context.Context, app *bootstrap.App) (domain.Movement, error) {
		svcIn, err := in.toService()
		if err != nil {
			return domain.Movement{}, err
		}
		return app.Stock.Receive(ctx, svcIn)
	})
}

// Opening books the stock a shop had when it adopted Lite — a product's first movement only.
func (s *Stock) Opening(in ReceiveInput) envelope.Result[StockLevelDTO] {
	return s.act("Stock.Opening", func(ctx context.Context, app *bootstrap.App) (domain.Movement, error) {
		svcIn, err := in.toService()
		if err != nil {
			return domain.Movement{}, err
		}
		return app.Stock.Opening(ctx, svcIn)
	})
}

// Count records what is on the shelf. A count that lowers stock returns lite.owner.required outside owner mode.
func (s *Stock) Count(in CountInput) envelope.Result[StockLevelDTO] {
	return s.act("Stock.Count", func(ctx context.Context, app *bootstrap.App) (domain.Movement, error) {
		productID, err := parseProductID(in.ProductID)
		if err != nil {
			return domain.Movement{}, err
		}
		return app.Stock.Count(ctx, stock.CountInput{ProductID: productID, Counted: in.Counted, Note: in.Note})
	})
}

// Adjust adds or writes off stock. A write-off returns lite.owner.required outside owner mode.
func (s *Stock) Adjust(in AdjustInput) envelope.Result[StockLevelDTO] {
	return s.act("Stock.Adjust", func(ctx context.Context, app *bootstrap.App) (domain.Movement, error) {
		productID, err := parseProductID(in.ProductID)
		if err != nil {
			return domain.Movement{}, err
		}
		return app.Stock.Adjust(ctx, stock.AdjustInput{
			ProductID: productID, Direction: stock.Direction(in.Direction), Quantity: in.Quantity,
			Reason: domain.Reason(in.Reason), Note: in.Note,
		})
	})
}

// OpenPackage opens whole packages and returns both products' quantities after: the package's, then the content's.
func (s *Stock) OpenPackage(in OpenPackageInput) envelope.Result[[]StockLevelDTO] {
	return call(s.core, "Stock.OpenPackage", func(ctx context.Context, app *bootstrap.App) ([]StockLevelDTO, error) {
		productID, err := parseProductID(in.PackageProductID)
		if err != nil {
			return nil, err
		}
		opened, err := app.Stock.OpenPackage(ctx, stock.OpenPackageInput{PackageProductID: productID, Packages: in.Packages})
		if err != nil {
			return nil, err
		}
		out := make([]StockLevelDTO, 0, 2)
		for _, m := range []domain.Movement{opened.Out, opened.In} {
			level, err := levelAfter(ctx, app, m)
			if err != nil {
				return nil, err
			}
			out = append(out, level)
		}
		return out, nil
	})
}

// ReverseReceipt takes back the product's newest receipt. Owner only.
func (s *Stock) ReverseReceipt(in ReverseReceiptInput) envelope.Result[StockLevelDTO] {
	return s.act("Stock.ReverseReceipt", func(ctx context.Context, app *bootstrap.App) (domain.Movement, error) {
		movementID, err := id.Parse(in.MovementID)
		if err != nil {
			return domain.Movement{}, domain.ErrMovementNotFound()
		}
		return app.Stock.ReverseReceipt(ctx, movementID, in.Note)
	})
}

// CorrectCost sets a product's average cost. Owner only.
func (s *Stock) CorrectCost(in CorrectCostInput) envelope.Result[StockLevelDTO] {
	return s.act("Stock.CorrectCost", func(ctx context.Context, app *bootstrap.App) (domain.Movement, error) {
		productID, err := parseProductID(in.ProductID)
		if err != nil {
			return domain.Movement{}, err
		}
		return app.Stock.CorrectCost(ctx, stock.CorrectCostInput{ProductID: productID, AverageCost: in.AverageCost, Note: in.Note})
	})
}

// Verify checks the ledger and reports what it found. Owner mode only.
func (s *Stock) Verify() envelope.Result[[]FindingDTO] {
	return call(s.core, "Stock.Verify", func(ctx context.Context, app *bootstrap.App) ([]FindingDTO, error) {
		findings, err := app.Stock.Verify(ctx)
		out := make([]FindingDTO, 0, len(findings))
		for _, f := range findings {
			out = append(out, FindingDTO{Code: f.Code, ProductID: f.ProductID.String(), MovementID: f.MovementID.String()})
		}
		return out, err
	})
}

// act runs a stock act and answers with the product's quantity after it — never a cost, so an act's reply cannot show
// what a screen outside owner mode may not.
func (s *Stock) act(method string, fn func(ctx context.Context, app *bootstrap.App) (domain.Movement, error)) envelope.Result[StockLevelDTO] {
	return call(s.core, method, func(ctx context.Context, app *bootstrap.App) (StockLevelDTO, error) {
		m, err := fn(ctx, app)
		if err != nil {
			return StockLevelDTO{}, err
		}
		return levelAfter(ctx, app, m)
	})
}

func levelAfter(ctx context.Context, app *bootstrap.App, m domain.Movement) (StockLevelDTO, error) {
	p, err := app.Catalog.Get(ctx, m.ProductID)
	if err != nil {
		return StockLevelDTO{}, err
	}
	ref, err := app.Catalog.Reference(ctx)
	if err != nil {
		return StockLevelDTO{}, err
	}
	return toLevelDTO(stock.OnHand{ProductID: m.ProductID, OnHandMicro: m.OnHandAfterMicro, UnitDecimals: ref.Units[p.UnitCode].InputDecimals}), nil
}

func currencyDecimals(ctx context.Context, app *bootstrap.App) (map[string]int, error) {
	currencies, err := app.Catalog.Currencies(ctx)
	out := make(map[string]int, len(currencies))
	for _, c := range currencies {
		out[c.Code] = c.Decimals
	}
	return out, err
}
