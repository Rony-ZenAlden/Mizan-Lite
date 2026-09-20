package sales

import (
	"context"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bizdate"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

// Partial sales returns (the owner's request, 2026-09-20).
//
// A return is quoted and then recorded, like a sale: the person at the counter sees what the customer is owed before
// anything moves, and the recording re-prices from the sale rather than trusting the figures the screen sent back.

// ReturnStore is the part of Store that holds returns. It is declared here rather than added to Store so the module's
// existing fake and its contract suite keep compiling unchanged where returns are not in play.
type ReturnStore interface {
	NextReturnNo(ctx context.Context) (int64, error)
	InsertReturn(ctx context.Context, r domain.Return) error
	// ReturnedOf is how much of each of a sale's lines earlier returns already took back.
	ReturnedOf(ctx context.Context, saleID id.ID) (domain.Returned, error)
	GetReturn(ctx context.Context, returnID id.ID) (domain.Return, error)
	ReturnsRange(ctx context.Context, from, to string) ([]domain.Return, error)
	ReturnsOfSale(ctx context.Context, saleID id.ID) ([]domain.Return, error)
}

// ReturnStock is the stock this module needs for a return: putting part of a sold line back on the shelf.
type ReturnStock interface {
	RecordSaleReturn(ctx context.Context, saleLineID id.ID, quantityMicro int64) error
}

// ReturnDebts is the debt ledger's part: taking a return off what a customer owes.
type ReturnDebts interface {
	RecordSaleReturn(ctx context.Context, in ReturnDebtInput) (id.ID, error)
}

// ReturnDebtInput is a return settled against a customer's balance.
type ReturnDebtInput struct {
	CustomerID   id.ID
	Currency     string
	AmountMinor  int64
	Reason       string
	BusinessDate string
	At           time.Time
}

// UseReturns gives the service the three ports a return needs. The composition root calls it once.
func (s *Service) UseReturns(store ReturnStore, stock ReturnStock, debts ReturnDebts) {
	s.returns, s.returnStock, s.returnDebts = store, stock, debts
}

// ReturnInput is a return as a screen asks for it.
type ReturnInput struct {
	SaleID id.ID
	Draft  domain.ReturnDraft
}

// QuoteReturn prices a return without recording anything, so the counter can say what the customer is owed before the
// goods change hands.
func (s *Service) QuoteReturn(ctx context.Context, in ReturnInput) (domain.Return, error) {
	if s.returns == nil {
		return domain.Return{}, errs.Internal(domain.CodeReturnNotFound, "returns are not wired")
	}
	sale, err := s.store.Get(ctx, in.SaleID)
	if err != nil {
		return domain.Return{}, err
	}
	return s.priceReturn(ctx, sale, in.Draft)
}

// ReturnableOf is a sale with what each of its lines has already had returned — what the F8 wizard opens on.
func (s *Service) ReturnableOf(ctx context.Context, saleID id.ID) (domain.Sale, domain.Returned, error) {
	sale, err := s.store.Get(ctx, saleID)
	if err != nil {
		return domain.Sale{}, nil, err
	}
	if sale.Payment == domain.PaymentCredit {
		if sale, err = s.withCredit(ctx, sale); err != nil {
			return domain.Sale{}, nil, err
		}
	}
	already, err := s.returns.ReturnedOf(ctx, saleID)
	return sale, already, err
}

// ByReceiptNo finds a sale by the number printed on its receipt — how a person at the counter has it, holding the
// paper the customer handed back.
func (s *Service) ByReceiptNo(ctx context.Context, receiptNo int64) (domain.Sale, error) {
	finder, ok := s.store.(interface {
		GetByReceiptNo(ctx context.Context, receiptNo int64) (domain.Sale, error)
	})
	if !ok {
		return domain.Sale{}, errs.Internal(domain.CodeSaleNotFound, "this store cannot find a sale by its number")
	}
	return finder.GetByReceiptNo(ctx, receiptNo)
}

// Return records a return: the stock goes back, the money goes back, and the owner's history says it happened.
//
// It re-prices from the stored sale rather than trusting what the screen sent, for the same reason checkout re-prices
// a cart: the figures a person saw are a claim, not an authority.
func (s *Service) Return(ctx context.Context, in ReturnInput) (domain.Return, error) {
	if s.returns == nil || s.returnStock == nil || s.returnDebts == nil {
		return domain.Return{}, errs.Internal(domain.CodeReturnNotFound, "returns are not wired")
	}
	var out domain.Return
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		sale, err := s.store.Get(ctx, in.SaleID)
		if err != nil {
			return err
		}
		if sale.Payment == domain.PaymentCredit {
			if sale, err = s.withCredit(ctx, sale); err != nil {
				return err
			}
		}
		priced, err := s.priceReturn(ctx, sale, in.Draft)
		if err != nil {
			return err
		}
		cur, err := s.currency(ctx, priced.SettlementCurrency)
		if err != nil {
			return err
		}
		// Money leaves the shop, so it is an owner's act — recorded whether or not the PIN was asked for.
		if err = s.gate.Require(ctx, GuardedAct{
			Action: domain.ActReturn, SubjectID: sale.ID,
			Before: "No. " + strconv.FormatInt(sale.ReceiptNo, 10) + " · " + moneyText(sale.TotalMinor, cur),
			After:  moneyText(priced.RefundMinor, cur) + " · " + priced.Reason,
		}); err != nil {
			return err
		}
		if priced.ReturnNo, err = s.returns.NextReturnNo(ctx); err != nil {
			return err
		}
		if priced.ID, err = s.newID(); err != nil {
			return err
		}
		for i := range priced.Lines {
			if priced.Lines[i].ID, err = s.newID(); err != nil {
				return err
			}
		}
		// The debt entry is written first so the return can name it.
		if priced.Settlement == domain.SettleDebt {
			priced.CustomerID = sale.Credit.CustomerID
			if priced.CustomerID.IsZero() {
				return errs.Conflict(domain.CodeReturnNotCredit, "this sale has no customer to credit")
			}
			if priced.DebtEntryID, err = s.returnDebts.RecordSaleReturn(ctx, ReturnDebtInput{
				CustomerID: priced.CustomerID, Currency: priced.SettlementCurrency, AmountMinor: priced.RefundMinor,
				Reason: priced.Reason, BusinessDate: priced.BusinessDate, At: priced.ReturnedAt,
			}); err != nil {
				return err
			}
		}
		if err = s.returns.InsertReturn(ctx, priced); err != nil {
			return err
		}
		// Only what is fit to sell again goes back on the shelf.
		for _, l := range priced.Lines {
			if !l.Restocked {
				continue
			}
			if err = s.returnStock.RecordSaleReturn(ctx, l.SaleLineID, l.QuantityMicro); err != nil {
				return err
			}
		}
		out = priced
		return nil
	})
	return out, err
}

// priceReturn gathers what the domain needs and prices the draft.
func (s *Service) priceReturn(ctx context.Context, sale domain.Sale, draft domain.ReturnDraft) (domain.Return, error) {
	already, err := s.returns.ReturnedOf(ctx, sale.ID)
	if err != nil {
		return domain.Return{}, err
	}
	rate, localCode, found, err := s.rates.InForce(ctx)
	if err != nil {
		return domain.Return{}, err
	}
	if !found {
		return domain.Return{}, errs.Conflict(domain.CodeNoRate, "no exchange rate is in force")
	}
	local, err := s.currency(ctx, localCode)
	if err != nil {
		return domain.Return{}, err
	}
	usd, err := s.currency(ctx, domain.USD)
	if err != nil {
		return domain.Return{}, err
	}
	units, err := s.unitDecimals(ctx, sale)
	if err != nil {
		return domain.Return{}, err
	}
	at := s.clk.Now().UTC().Truncate(time.Millisecond)
	return domain.PriceReturn(sale, already, draft, rate, local, usd, units, at, bizdate.Date(at, s.loc))
}

// unitDecimals is how many decimals each unit on the sale takes, read from the catalogue.
func (s *Service) unitDecimals(ctx context.Context, sale domain.Sale) (map[string]int, error) {
	out := map[string]int{}
	for _, l := range sale.Lines {
		if _, seen := out[l.UnitCode]; seen {
			continue
		}
		p, err := s.catalogue.Product(ctx, l.ProductID)
		if err != nil {
			// The product is gone from the catalogue but its line is not: the sale's snapshot is what the return
			// prices, and a whole-number quantity is the safe reading of a unit nobody can look up any more.
			if errs.CodeOf(err) == domain.CodeUnknownProduct {
				out[l.UnitCode] = 0
				continue
			}
			return nil, err
		}
		out[l.UnitCode] = p.UnitDecimals
	}
	return out, nil
}

// ReturnsOfSale is every return recorded against one sale.
func (s *Service) ReturnsOfSale(ctx context.Context, saleID id.ID) ([]domain.Return, error) {
	return s.returns.ReturnsOfSale(ctx, saleID)
}

// GetReturn is one return with its lines.
func (s *Service) GetReturn(ctx context.Context, returnID id.ID) (domain.Return, error) {
	return s.returns.GetReturn(ctx, returnID)
}

// ReturnsBetween is every return recorded on a business date from..to, for the reports.
func (s *Service) ReturnsBetween(ctx context.Context, from, to string) ([]domain.Return, error) {
	return s.returns.ReturnsRange(ctx, from, to)
}
