package api

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
	"github.com/mizan-erp/mizan/internal/lite/tender"
)

// Suppliers is the payables book (0.10.0): the suppliers a shop buys from, its purchases on cash or credit with their
// discounts and damaged goods, and the money it pays them — kept apart from the customers' debts.
//
// A balance above nought is what the shop owes the supplier; below nought, what the supplier owes the shop. Balances
// cross per currency, never summed across currencies, as Go-formatted strings (DESIGN D9).
type Suppliers struct{ core *core }

// SupplierBalanceDTO is one currency's side of a supplier, or of all of them.
type SupplierBalanceDTO struct {
	Currency string `json:"currency"`
	Balance  string `json:"balance"`
}

// SupplierDTO is a supplier with a balance per currency they have entries in.
type SupplierDTO struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Phone      string               `json:"phone"`
	City       string               `json:"city"`
	Note       string               `json:"note"`
	Active     bool                 `json:"active"`
	RowVersion int64                `json:"rowVersion"`
	Balances   []SupplierBalanceDTO `json:"balances"`
}

// SupplierQueryDTO searches suppliers by name or phone.
type SupplierQueryDTO struct {
	Text            string `json:"text"`
	IncludeInactive bool   `json:"includeInactive"`
}

// SupplierListDTO is the suppliers found and what the shop owes all of them, per currency. Rate is the rate in force as
// the shop reads it — what a purchase in the local currency starts from — or "".
type SupplierListDTO struct {
	Suppliers     []SupplierDTO        `json:"suppliers"`
	Totals        []SupplierBalanceDTO `json:"totals"`
	LocalCurrency string               `json:"localCurrency"`
	Rate          string               `json:"rate"`
}

// SupplierInput is a new supplier.
type SupplierInput struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	City  string `json:"city"`
	Note  string `json:"note"`
}

// UpdateSupplierInput edits a supplier at a version.
type UpdateSupplierInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	City       string `json:"city"`
	Note       string `json:"note"`
}

// SetSupplierActiveInput activates or deactivates a supplier.
type SetSupplierActiveInput struct {
	ID         string `json:"id"`
	RowVersion int64  `json:"rowVersion"`
	Active     bool   `json:"active"`
}

// SupplierStatementQueryDTO asks for one supplier's book in one currency.
type SupplierStatementQueryDTO struct {
	SupplierID string `json:"supplierId"`
	Currency   string `json:"currency"`
}

// SupplierEntryDTO is one line of a supplier's book. Kind is opening, purchase, payment, refund or reversal; Source is
// "drawer" or "owner" for money that moved, "" otherwise.
type SupplierEntryDTO struct {
	ID           string `json:"id"`
	Seq          int64  `json:"seq"`
	BusinessDate string `json:"businessDate"`
	OccurredAt   string `json:"occurredAt"`
	Kind         string `json:"kind"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	BalanceAfter string `json:"balanceAfter"`
	Source       string `json:"source"`
	PurchaseID   string `json:"purchaseId"`
	// PurchaseNo and SupplierRef name the purchase an entry belongs to; 0 and "" otherwise.
	PurchaseNo  int64  `json:"purchaseNo"`
	SupplierRef string `json:"supplierRef"`
	ReversesID  string `json:"reversesId"`
	Note        string `json:"note"`
	// Reversed is true once reversed; Reversible when the owner may reverse it — a payment, a refund or an opening balance
	// not yet reversed. A purchase is undone by voiding it.
	Reversed   bool `json:"reversed"`
	Reversible bool `json:"reversible"`
}

// SupplierStatementDTO is one supplier's book in one currency, newest first.
type SupplierStatementDTO struct {
	Supplier SupplierDTO        `json:"supplier"`
	Currency string             `json:"currency"`
	Balance  string             `json:"balance"`
	Entries  []SupplierEntryDTO `json:"entries"`
}

// PurchaseLineInput is one line of a purchase as typed: the units that arrived, how many of them damaged, the price of
// one unit, and a discount as a percentage OR an amount off the line.
type PurchaseLineInput struct {
	ProductID       string `json:"productId"`
	Quantity        string `json:"quantity"`
	Damaged         string `json:"damaged"`
	UnitCost        string `json:"unitCost"`
	DiscountPercent string `json:"discountPercent"`
	DiscountAmount  string `json:"discountAmount"`
}

// PurchaseInput is a purchase as typed. Rate is required for a purchase in the local currency; PaidFrom ("drawer" or
// "owner") for money paid on the spot.
type PurchaseInput struct {
	SupplierID      string              `json:"supplierId"`
	Currency        string              `json:"currency"`
	Rate            string              `json:"rate"`
	SupplierRef     string              `json:"supplierRef"`
	Lines           []PurchaseLineInput `json:"lines"`
	InvoiceDiscount string              `json:"invoiceDiscount"`
	PaidNow         string              `json:"paidNow"`
	PaidFrom        string              `json:"paidFrom"`
	Note            string              `json:"note"`
}

// PurchaseLineDTO is one priced line. Good is what was received into stock; NetUnitCost what one good unit cost after
// every discount — the cost the stock book holds.
type PurchaseLineDTO struct {
	LineNo          int    `json:"lineNo"`
	ProductID       string `json:"productId"`
	NameAR          string `json:"nameAr"`
	NameEN          string `json:"nameEn"`
	UnitCode        string `json:"unitCode"`
	Quantity        string `json:"quantity"`
	Damaged         string `json:"damaged"`
	Good            string `json:"good"`
	UnitCost        string `json:"unitCost"`
	DiscountPercent string `json:"discountPercent"`
	Gross           string `json:"gross"`
	LineDiscount    string `json:"lineDiscount"`
	InvoiceShare    string `json:"invoiceShare"`
	Due             string `json:"due"`
	NetUnitCost     string `json:"netUnitCost"`
}

// PurchaseDTO is a purchase as recorded (or quoted). Status is "posted" or "voided".
type PurchaseDTO struct {
	ID              string            `json:"id"`
	PurchaseNo      int64             `json:"purchaseNo"`
	SupplierID      string            `json:"supplierId"`
	SupplierName    string            `json:"supplierName"`
	BusinessDate    string            `json:"businessDate"`
	OccurredAt      string            `json:"occurredAt"`
	Currency        string            `json:"currency"`
	Rate            string            `json:"rate"`
	SupplierRef     string            `json:"supplierRef"`
	Lines           []PurchaseLineDTO `json:"lines"`
	Gross           string            `json:"gross"`
	LineDiscount    string            `json:"lineDiscount"`
	InvoiceDiscount string            `json:"invoiceDiscount"`
	Due             string            `json:"due"`
	PaidNow         string            `json:"paidNow"`
	PaidFrom        string            `json:"paidFrom"`
	Status          string            `json:"status"`
	VoidedAt        string            `json:"voidedAt"`
	VoidReason      string            `json:"voidReason"`
	Note            string            `json:"note"`
	DamagedLines    int               `json:"damagedLines"`
}

// PurchaseQuoteDTO is a purchase worked out by Go and not recorded, with the supplier's balance before and after.
type PurchaseQuoteDTO struct {
	Purchase      PurchaseDTO `json:"purchase"`
	BalanceBefore string      `json:"balanceBefore"`
	BalanceAfter  string      `json:"balanceAfter"`
}

// PurchaseQueryDTO lists purchases, newest first: one supplier's, a range of business dates, or both.
type PurchaseQueryDTO struct {
	SupplierID string `json:"supplierId"`
	From       string `json:"from"`
	To         string `json:"to"`
	Limit      int    `json:"limit"`
}

// VoidPurchaseInput voids a purchase with its reason.
type VoidPurchaseInput struct {
	PurchaseID string `json:"purchaseId"`
	Reason     string `json:"reason"`
}

// SupplierMoneyInput is a payment to a supplier, a refund from one, or an opening balance. Source is "drawer" or "owner"
// (not for an opening balance); InShopsFavour makes an opening balance one the supplier owes the shop.
type SupplierMoneyInput struct {
	SupplierID    string `json:"supplierId"`
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
	Source        string `json:"source"`
	InShopsFavour bool   `json:"inShopsFavour"`
	Note          string `json:"note"`
}

// SupplierReverseInput reverses an entry made by mistake.
type SupplierReverseInput struct {
	EntryID string `json:"entryId"`
	Reason  string `json:"reason"`
}

// supplierView formats the payables book against the reference data and how the shop reads its money.
type supplierView struct{ tillView }

func newSupplierView(ctx context.Context, app *bootstrap.App) (supplierView, error) {
	v, err := newTillView(ctx, app)
	return supplierView{tillView: v}, err
}

func (v supplierView) decimals(code string) int { return v.ref.Currencies[code].Decimals }

// unitMoney formats a figure at 10⁻⁶ of a currency — a unit cost — with at least the currency's decimals.
func (v supplierView) unitMoney(micro int64, code string) string {
	return v.shop.Display(numinput.FormatFixed(micro, 6, v.decimals(code)), code)
}

func (v supplierView) supplier(w suppliers.WithBalances) SupplierDTO {
	s := w.Supplier
	dto := SupplierDTO{ID: s.ID.String(), Name: s.Name, Phone: s.Phone, City: s.City, Note: s.Note, Active: s.Active,
		RowVersion: s.RowVersion, Balances: make([]SupplierBalanceDTO, 0, len(w.Summaries))}
	for _, sum := range w.Summaries {
		dto.Balances = append(dto.Balances, SupplierBalanceDTO{Currency: sum.Currency, Balance: v.money(sum.BalanceMinor, sum.Currency)})
	}
	return dto
}

type purchaseRef struct {
	no  int64
	ref string
}

func (v supplierView) entry(e domain.Entry, reversed bool, purchases map[id.ID]purchaseRef) SupplierEntryDTO {
	p := purchases[e.PurchaseID]
	return SupplierEntryDTO{
		ID: e.ID.String(), Seq: e.Seq, BusinessDate: e.BusinessDate, OccurredAt: clock.Format(e.OccurredAt), Kind: string(e.Kind),
		Currency: e.Currency, Amount: v.money(e.AmountMinor, e.Currency), BalanceAfter: v.money(e.BalanceAfterMinor, e.Currency),
		Source: string(e.Source), PurchaseID: e.PurchaseID.String(), PurchaseNo: p.no, SupplierRef: p.ref,
		ReversesID: e.ReversesID.String(), Note: e.Note, Reversed: reversed,
		Reversible: !reversed && (e.Kind == domain.KindPayment || e.Kind == domain.KindRefund || e.Kind == domain.KindOpening),
	}
}

func (v supplierView) purchase(p domain.Purchase) PurchaseDTO {
	c := p.Currency
	dto := PurchaseDTO{
		ID: p.ID.String(), PurchaseNo: p.PurchaseNo, SupplierID: p.SupplierID.String(), SupplierName: p.SupplierName,
		BusinessDate: p.BusinessDate, Currency: c, SupplierRef: p.SupplierRef, Lines: make([]PurchaseLineDTO, 0, len(p.Lines)),
		Gross: v.money(p.GrossMinor, c), LineDiscount: v.money(p.LineDiscountMinor, c), InvoiceDiscount: v.money(p.InvoiceDiscountMinor, c),
		Due: v.money(p.DueMinor(), c), PaidNow: v.money(p.PaidNowMinor, c), PaidFrom: string(p.PaidFrom), Status: string(p.Status),
		VoidReason: p.VoidReason, Note: p.Note, DamagedLines: p.DamagedLines(),
	}
	if !p.OccurredAt.IsZero() {
		dto.OccurredAt = clock.Format(p.OccurredAt)
	}
	if !p.VoidedAt.IsZero() {
		dto.VoidedAt = clock.Format(p.VoidedAt)
	}
	if p.RateNano > 0 {
		dto.Rate = rateText(v.shop, p.RateNano)
	}
	for _, l := range p.Lines {
		line := PurchaseLineDTO{
			LineNo: l.LineNo, ProductID: l.ProductID.String(), NameAR: l.NameAR, NameEN: l.NameEN, UnitCode: l.UnitCode,
			Quantity: v.quantity(l.QuantityMicro, l.UnitCode), Damaged: v.quantity(l.DamagedMicro, l.UnitCode),
			Good: v.quantity(l.GoodMicro(), l.UnitCode), UnitCost: v.unitMoney(l.UnitCostMicro, c),
			Gross: v.money(l.GrossMinor, c), LineDiscount: v.money(l.LineDiscountMinor, c), InvoiceShare: v.money(l.InvoiceShareMinor, c),
			Due: v.money(l.DueMinor(), c), NetUnitCost: v.unitMoney(l.NetUnitMicro(v.decimals(c)), c),
		}
		if l.DiscountPercentMicro > 0 {
			line.DiscountPercent = numinput.FormatFixed(l.DiscountPercentMicro, 6, 0)
		}
		dto.Lines = append(dto.Lines, line)
	}
	return dto
}

// toService takes a purchase back into the pounds the books hold: every figure in the purchase's currency, and the rate —
// local pounds per dollar — always in the local one (as a delivery's, 0.9.9).
func (in PurchaseInput) toService(shop moneyfmt.Shop) (domain.Input, error) {
	supplierID, err := parseSupplierID(in.SupplierID)
	if err != nil {
		return domain.Input{}, err
	}
	out := domain.Input{SupplierID: supplierID, Currency: in.Currency, Rate: shop.Base(in.Rate, shop.Local), SupplierRef: in.SupplierRef,
		InvoiceDiscount: shop.Base(in.InvoiceDiscount, in.Currency), PaidNow: shop.Base(in.PaidNow, in.Currency), PaidFrom: in.PaidFrom,
		Note: in.Note, Lines: make([]domain.LineInput, 0, len(in.Lines))}
	for _, l := range in.Lines {
		// An unreadable product id is an unknown product, which the purchase refuses naming the line.
		productID, _ := id.Parse(l.ProductID)
		out.Lines = append(out.Lines, domain.LineInput{ProductID: productID, Quantity: l.Quantity, Damaged: l.Damaged,
			UnitCost: shop.Base(l.UnitCost, in.Currency), DiscountPercent: l.DiscountPercent,
			DiscountAmount: shop.Base(l.DiscountAmount, in.Currency)})
	}
	return out, nil
}

func parseSupplierID(raw string) (id.ID, error) {
	parsed, err := id.Parse(raw)
	if err != nil {
		return "", domain.ErrNotFound()
	}
	return parsed, nil
}

func parsePurchaseID(raw string) (id.ID, error) {
	parsed, err := id.Parse(raw)
	if err != nil {
		return "", domain.ErrPurchaseNotFound()
	}
	return parsed, nil
}

// List finds suppliers by name in any spelling, or by phone in any digits, with their balances and what the shop owes
// them all. Outside owner mode with the PIN switch on it returns lite.owner.required.
func (s *Suppliers) List(q SupplierQueryDTO) envelope.Result[SupplierListDTO] {
	return call(s.core, "Suppliers.List", func(ctx context.Context, app *bootstrap.App) (SupplierListDTO, error) {
		found, err := app.Suppliers.Search(ctx, q.Text, q.IncludeInactive)
		if err != nil {
			return SupplierListDTO{}, err
		}
		totals, err := app.Suppliers.Payables(ctx)
		if err != nil {
			return SupplierListDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		if err != nil {
			return SupplierListDTO{}, err
		}
		current, err := app.FX.Current(ctx)
		if err != nil {
			return SupplierListDTO{}, err
		}
		out := SupplierListDTO{Suppliers: make([]SupplierDTO, 0, len(found)), Totals: []SupplierBalanceDTO{}, LocalCurrency: current.Local}
		if current.Found {
			out.Rate = rateText(v.shop, current.Rate.Nano)
		}
		for _, w := range found {
			out.Suppliers = append(out.Suppliers, v.supplier(w))
		}
		for code, minor := range totals {
			if minor != 0 {
				out.Totals = append(out.Totals, SupplierBalanceDTO{Currency: code, Balance: v.money(minor, code)})
			}
		}
		// Dollars last, as a supplier's own balances read.
		sort.Slice(out.Totals, func(i, j int) bool {
			if (out.Totals[i].Currency == tender.USD) != (out.Totals[j].Currency == tender.USD) {
				return out.Totals[j].Currency == tender.USD
			}
			return out.Totals[i].Currency < out.Totals[j].Currency
		})
		return out, nil
	})
}

// withSupplier formats one supplier, with balances, after an act.
func (s *Suppliers) withSupplier(method string, fn func(ctx context.Context, app *bootstrap.App) (domain.Supplier, error)) envelope.Result[SupplierDTO] {
	return call(s.core, method, func(ctx context.Context, app *bootstrap.App) (SupplierDTO, error) {
		sup, err := fn(ctx, app)
		if err != nil {
			return SupplierDTO{}, err
		}
		w, err := app.Suppliers.Get(ctx, sup.ID)
		if err != nil {
			return SupplierDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		return v.supplier(w), err
	})
}

// Create adds a supplier.
func (s *Suppliers) Create(in SupplierInput) envelope.Result[SupplierDTO] {
	return s.withSupplier("Suppliers.Create", func(ctx context.Context, app *bootstrap.App) (domain.Supplier, error) {
		return app.Suppliers.Create(ctx, domain.Draft{Name: in.Name, Phone: in.Phone, City: in.City, Note: in.Note})
	})
}

// Update edits a supplier's name, phone, city and note.
func (s *Suppliers) Update(in UpdateSupplierInput) envelope.Result[SupplierDTO] {
	return s.withSupplier("Suppliers.Update", func(ctx context.Context, app *bootstrap.App) (domain.Supplier, error) {
		supplierID, err := parseSupplierID(in.ID)
		if err != nil {
			return domain.Supplier{}, err
		}
		return app.Suppliers.Update(ctx, suppliers.UpdateInput{ID: supplierID, RowVersion: in.RowVersion,
			Draft: domain.Draft{Name: in.Name, Phone: in.Phone, City: in.City, Note: in.Note}})
	})
}

// SetActive activates or deactivates a supplier. An inactive supplier keeps its book and is left out of the pickers.
func (s *Suppliers) SetActive(in SetSupplierActiveInput) envelope.Result[SupplierDTO] {
	return s.withSupplier("Suppliers.SetActive", func(ctx context.Context, app *bootstrap.App) (domain.Supplier, error) {
		supplierID, err := parseSupplierID(in.ID)
		if err != nil {
			return domain.Supplier{}, err
		}
		return app.Suppliers.SetActive(ctx, supplierID, in.RowVersion, in.Active)
	})
}

// Statement is one supplier's book in one currency, newest first.
func (s *Suppliers) Statement(q SupplierStatementQueryDTO) envelope.Result[SupplierStatementDTO] {
	return call(s.core, "Suppliers.Statement", func(ctx context.Context, app *bootstrap.App) (SupplierStatementDTO, error) {
		supplierID, err := parseSupplierID(q.SupplierID)
		if err != nil {
			return SupplierStatementDTO{}, err
		}
		st, err := app.Suppliers.StatementOf(ctx, supplierID, q.Currency)
		if err != nil {
			return SupplierStatementDTO{}, err
		}
		w, err := app.Suppliers.Get(ctx, supplierID)
		if err != nil {
			return SupplierStatementDTO{}, err
		}
		bought, err := app.Suppliers.Purchases(ctx, suppliers.PurchaseQuery{SupplierID: supplierID})
		if err != nil {
			return SupplierStatementDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		if err != nil {
			return SupplierStatementDTO{}, err
		}
		refs := make(map[id.ID]purchaseRef, len(bought))
		for _, p := range bought {
			refs[p.ID] = purchaseRef{no: p.PurchaseNo, ref: p.SupplierRef}
		}
		reversed := map[id.ID]bool{}
		for _, e := range st.Entries {
			if e.ReversesID != "" {
				reversed[e.ReversesID] = true
			}
		}
		dto := SupplierStatementDTO{Supplier: v.supplier(w), Currency: st.Currency, Balance: v.money(st.BalanceMinor, st.Currency),
			Entries: make([]SupplierEntryDTO, 0, len(st.Entries))}
		for i := len(st.Entries) - 1; i >= 0; i-- {
			e := st.Entries[i]
			dto.Entries = append(dto.Entries, v.entry(e, reversed[e.ID], refs))
		}
		return dto, nil
	})
}

// QuotePurchase works a purchase out from what was typed and records nothing — the form's figures, from Go.
func (s *Suppliers) QuotePurchase(in PurchaseInput) envelope.Result[PurchaseQuoteDTO] {
	return call(s.core, "Suppliers.QuotePurchase", func(ctx context.Context, app *bootstrap.App) (PurchaseQuoteDTO, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return PurchaseQuoteDTO{}, err
		}
		svcIn, err := in.toService(shop)
		if err != nil {
			return PurchaseQuoteDTO{}, err
		}
		q, err := app.Suppliers.QuotePurchase(ctx, svcIn)
		if err != nil {
			return PurchaseQuoteDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		if err != nil {
			return PurchaseQuoteDTO{}, err
		}
		c := q.Purchase.Currency
		return PurchaseQuoteDTO{Purchase: v.purchase(q.Purchase), BalanceBefore: v.money(q.BalanceBeforeMinor, c),
			BalanceAfter: v.money(q.BalanceAfterMinor, c)}, nil
	})
}

// withPurchase formats one purchase after an act.
func (s *Suppliers) withPurchase(method string, fn func(ctx context.Context, app *bootstrap.App) (domain.Purchase, error)) envelope.Result[PurchaseDTO] {
	return call(s.core, method, func(ctx context.Context, app *bootstrap.App) (PurchaseDTO, error) {
		p, err := fn(ctx, app)
		if err != nil {
			return PurchaseDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		return v.purchase(p), err
	})
}

// RecordPurchase records a purchase: the good units received into stock at what they cost after every discount, and
// the supplier's book charged with it. The owner's act — outside owner mode with the PIN switch on it returns
// lite.owner.required.
func (s *Suppliers) RecordPurchase(in PurchaseInput) envelope.Result[PurchaseDTO] {
	return s.withPurchase("Suppliers.RecordPurchase", func(ctx context.Context, app *bootstrap.App) (domain.Purchase, error) {
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return domain.Purchase{}, err
		}
		svcIn, err := in.toService(shop)
		if err != nil {
			return domain.Purchase{}, err
		}
		return app.Suppliers.RecordPurchase(ctx, svcIn)
	})
}

// VoidPurchase undoes a purchase entered by mistake. The owner's act.
func (s *Suppliers) VoidPurchase(in VoidPurchaseInput) envelope.Result[PurchaseDTO] {
	return s.withPurchase("Suppliers.VoidPurchase", func(ctx context.Context, app *bootstrap.App) (domain.Purchase, error) {
		purchaseID, err := parsePurchaseID(in.PurchaseID)
		if err != nil {
			return domain.Purchase{}, err
		}
		return app.Suppliers.VoidPurchase(ctx, purchaseID, in.Reason)
	})
}

// Purchase is one purchase with its lines.
func (s *Suppliers) Purchase(purchaseID string) envelope.Result[PurchaseDTO] {
	return s.withPurchase("Suppliers.Purchase", func(ctx context.Context, app *bootstrap.App) (domain.Purchase, error) {
		parsed, err := parsePurchaseID(purchaseID)
		if err != nil {
			return domain.Purchase{}, err
		}
		return app.Suppliers.Purchase(ctx, parsed)
	})
}

// Purchases lists purchases, newest first.
func (s *Suppliers) Purchases(q PurchaseQueryDTO) envelope.Result[[]PurchaseDTO] {
	return call(s.core, "Suppliers.Purchases", func(ctx context.Context, app *bootstrap.App) ([]PurchaseDTO, error) {
		query := suppliers.PurchaseQuery{From: q.From, To: q.To, Limit: q.Limit}
		if q.SupplierID != "" {
			supplierID, err := parseSupplierID(q.SupplierID)
			if err != nil {
				return nil, err
			}
			query.SupplierID = supplierID
		}
		found, err := app.Suppliers.Purchases(ctx, query)
		if err != nil {
			return nil, err
		}
		v, err := newSupplierView(ctx, app)
		if err != nil {
			return nil, err
		}
		out := make([]PurchaseDTO, 0, len(found))
		for _, p := range found {
			out = append(out, v.purchase(p))
		}
		return out, nil
	})
}

// money records a payment, refund or opening balance and formats the entry.
func (s *Suppliers) money(method string, in SupplierMoneyInput,
	fn func(ctx context.Context, app *bootstrap.App, in suppliers.MoneyInput) (domain.Entry, error)) envelope.Result[SupplierEntryDTO] {
	return call(s.core, method, func(ctx context.Context, app *bootstrap.App) (SupplierEntryDTO, error) {
		supplierID, err := parseSupplierID(in.SupplierID)
		if err != nil {
			return SupplierEntryDTO{}, err
		}
		shop, err := moneyShop(ctx, app)
		if err != nil {
			return SupplierEntryDTO{}, err
		}
		e, err := fn(ctx, app, suppliers.MoneyInput{SupplierID: supplierID, Currency: in.Currency, Amount: shop.Base(in.Amount, in.Currency),
			Source: in.Source, InShopsFavour: in.InShopsFavour, Note: in.Note})
		if err != nil {
			return SupplierEntryDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		return v.entry(e, false, nil), err
	})
}

// Pay records money paid to a supplier, from the drawer or the owner's own money. The owner's act.
func (s *Suppliers) Pay(in SupplierMoneyInput) envelope.Result[SupplierEntryDTO] {
	return s.money("Suppliers.Pay", in, func(ctx context.Context, app *bootstrap.App, m suppliers.MoneyInput) (domain.Entry, error) {
		return app.Suppliers.Pay(ctx, m)
	})
}

// Refund records money a supplier paid back against what they owed the shop.
func (s *Suppliers) Refund(in SupplierMoneyInput) envelope.Result[SupplierEntryDTO] {
	return s.money("Suppliers.Refund", in, func(ctx context.Context, app *bootstrap.App, m suppliers.MoneyInput) (domain.Entry, error) {
		return app.Suppliers.Refund(ctx, m)
	})
}

// Opening records a balance brought over from the paper book, either way. The owner's act.
func (s *Suppliers) Opening(in SupplierMoneyInput) envelope.Result[SupplierEntryDTO] {
	return s.money("Suppliers.Opening", in, func(ctx context.Context, app *bootstrap.App, m suppliers.MoneyInput) (domain.Entry, error) {
		return app.Suppliers.Opening(ctx, m)
	})
}

// Reverse undoes a payment, refund or opening balance entered by mistake. The owner's act.
func (s *Suppliers) Reverse(in SupplierReverseInput) envelope.Result[SupplierEntryDTO] {
	return call(s.core, "Suppliers.Reverse", func(ctx context.Context, app *bootstrap.App) (SupplierEntryDTO, error) {
		entryID, err := id.Parse(in.EntryID)
		if err != nil {
			return SupplierEntryDTO{}, domain.ErrEntryNotFound()
		}
		e, err := app.Suppliers.Reverse(ctx, entryID, in.Reason)
		if err != nil {
			return SupplierEntryDTO{}, err
		}
		v, err := newSupplierView(ctx, app)
		return v.entry(e, false, nil), err
	})
}
