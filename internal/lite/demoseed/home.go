package demoseed

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	cashbookdomain "github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	suppliersdomain "github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
)

// The furniture and home-goods demo (0.10.1, the owner's request of 2026-09-24): "الكردي — مفروشات وأدوات منزلية", a shop
// shown to a client. It sells in dollars only from its first day, and has a month and a half behind it: the stock it
// opened with, five suppliers — bought from on credit and in cash, with discounts and goods broken on arrival — eleven
// customers, a newlywed paying a bedroom off by the month among them, walk-in sales every day, expenses, breakages, a
// return, a void and the drawer counted at closing.
//
// Like the pantry demo it drives the services and never writes a row, so it is a shop the application could have produced
// — and it ends with the same verifiers and report reconciliations, which must find nothing. Nothing about it is in the
// application: its logo comes in through Options.Logo exactly as an owner's upload does, and its name and figures are
// data (data/home.json). The pantry demo stays the default: the end-to-end journeys run on it.

// The profiles.
const (
	ProfilePantry = "pantry"
	ProfileHome   = "home"
)

// CodeUnknownProfile refuses a profile this seeder does not have.
const CodeUnknownProfile = "lite.demoseed.unknown_profile"

// CodeHomeNeedsLogo and CodeHomeNeedsHistory refuse a home run without what it is made of: the shop's logo and the days
// before today.
const (
	CodeHomeNeedsLogo    = "lite.demoseed.home_needs_logo"
	CodeHomeNeedsHistory = "lite.demoseed.home_needs_history"
)

// HomeDays is the history the home demo is made for: every dated event in its story falls inside it.
const HomeDays = 45

type homeProduct struct {
	NameAR         string `json:"nameAr"`
	NameEN         string `json:"nameEn"`
	Unit           string `json:"unit"`
	Price          string `json:"price"`
	Cost           string `json:"cost"`
	Barcode        string `json:"barcode"`
	Opening        string `json:"opening"`
	UnitsPerCarton string `json:"unitsPerCarton"`
	// Kind is how the product sells: "big" furniture now and then, "mid" pieces, "small" goods every day.
	Kind      string `json:"kind"`
	QuickSlot int    `json:"quickSlot"`
}

type homeParty struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	City        string `json:"city"`
	Note        string `json:"note"`
	Opening     string `json:"opening"`
	OpeningNote string `json:"openingNote"`
}

type homeData struct {
	Shop struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		City    string `json:"city"`
		Address string `json:"address"`
		Footer  string `json:"footer"`
	} `json:"shop"`
	Rate      string        `json:"rate"`
	Products  []homeProduct `json:"products"`
	Suppliers []homeParty   `json:"suppliers"`
	Customers []homeParty   `json:"customers"`
}

// home is the home demo's run: the graph, the clock it steps and what it has made so far.
type home struct {
	ctx       context.Context
	app       *bootstrap.App
	pin       string
	opts      Options
	rng       *rand.Rand
	res       *Result
	products  []homeProduct
	byName    map[string]domain.Product
	kind      map[string]string
	suppliers map[string]suppliersdomain.Supplier
	customers map[string]customersdomain.Customer
	morning   time.Time
	// sold is each day's walk-in sales, for the void and the return the story makes.
	sold []salesdomain.Sale
}

func (h *home) wrap(err error, what string) error {
	if err == nil {
		return nil
	}
	return errs.Wrap(err, errs.CategoryInternal, errs.CodeOf(err), "seeding the home demo: "+what)
}

// owner enters the owner's PIN — before every act that is the owner's, as a person would, since owner mode runs out.
func (h *home) owner() error {
	_, err := h.app.Owner.Elevate(h.ctx, h.pin)
	return h.wrap(err, "the owner's PIN")
}

// at puts the clock at a time of the day being seeded.
func (h *home) at(hours, minutes int) {
	h.opts.Clock.Current = h.morning.Add(time.Duration(hours-9)*time.Hour + time.Duration(minutes)*time.Minute)
}

func (h *home) product(name string) (domain.Product, error) {
	p, ok := h.byName[name]
	if !ok {
		return domain.Product{}, errs.Internal(CodeStockInconsistent, "the home demo names a product it did not create").WithParam("product", name)
	}
	return p, nil
}

// onHand is how many whole units of a product are on the shelf.
func (h *home) onHand(p domain.Product) (int64, error) {
	st, err := h.app.Stock.Stocked(h.ctx, p.ID)
	if err != nil {
		return 0, err
	}
	return st.OnHandMicro / 1_000_000, nil
}

// runHome seeds the home demo. It needs the history (Days, Clock, Now) and the logo.
func runHome(ctx context.Context, app *bootstrap.App, opts Options) (Result, error) {
	if len(opts.Logo) == 0 {
		return Result{}, errs.Validation(CodeHomeNeedsLogo, "the home demo is seeded with its shop's logo")
	}
	if opts.Days < HomeDays || opts.Clock == nil {
		return Result{}, errs.Validation(CodeHomeNeedsHistory, "the home demo is a shop with a history").WithParam("days", strconv.Itoa(HomeDays))
	}
	var d homeData
	if err := readData("data/home.json", &d); err != nil {
		return Result{}, err
	}
	res := Result{ShopName: d.Shop.Name, Profile: ProfileHome}
	now := opts.Now.In(time.Local)
	start := time.Date(now.Year(), now.Month(), now.Day()-opts.Days, 0, 0, 0, 0, time.Local)
	h := &home{ctx: ctx, app: app, pin: opts.PIN, opts: opts, rng: rand.New(rand.NewSource(2026)), res: &res, products: d.Products, //nolint:gosec // a demo, reproducible
		byName: map[string]domain.Product{}, kind: map[string]string{}, suppliers: map[string]suppliersdomain.Supplier{},
		customers: map[string]customersdomain.Customer{}, morning: start.Add(9 * time.Hour)}
	h.at(9, 0)

	var err error
	if res.RecoveryCode, err = app.Setup.Run(ctx, setup.Input{ShopName: d.Shop.Name, Locale: opts.Locale, PIN: opts.PIN, Rate: d.Rate}); err != nil {
		return Result{}, h.wrap(err, "first run")
	}
	for _, step := range []func() error{
		func() error { return h.storeInformation(d) },
		h.catalogue,
		h.dollarsOnly,
		h.openings,
		func() error { return h.parties(d) },
	} {
		if err = step(); err != nil {
			return Result{}, err
		}
	}
	for day := range opts.Days {
		h.morning = start.AddDate(0, 0, day).Add(9 * time.Hour)
		if err = h.day(day); err != nil {
			return Result{}, err
		}
		res.HistoryDays++
	}
	h.morning = time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
	if err = h.today(opts.Now); err != nil {
		return Result{}, err
	}
	opts.Clock.Current = opts.Now
	if err = h.backup(); err != nil {
		return Result{}, err
	}
	if err = check(ctx, app, opts.PIN, &res); err != nil {
		return Result{}, err
	}
	if err = checkReports(ctx, app, opts.PIN, start.Format("2006-01-02"), &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// storeInformation is what heads every invoice: the name, the phone, the city and the address, the receipt's last line,
// and the logo — set as an owner uploads it. The printer is a name no computer has, printing nothing by itself, so a
// presentation is never interrupted by a printer that is not there; the owner chooses a real one on the Printer screen.
func (h *home) storeInformation(d homeData) error {
	if err := h.owner(); err != nil {
		return err
	}
	name, phone, city, address, footer := d.Shop.Name, d.Shop.Phone, d.Shop.City, d.Shop.Address, d.Shop.Footer
	printer, paper, path, auto, drawer := DemoPrinter, "80", "driver", "none", "false"
	if _, err := h.app.Settings.Update(h.ctx, settingsdomain.Update{ShopName: &name, Printing: settingsdomain.PrintingUpdate{
		Phone: &phone, City: &city, Address: &address, Footer: &footer,
		Printer: &printer, PaperMM: &paper, Path: &path, AutoPrint: &auto, Drawer: &drawer}}); err != nil {
		return h.wrap(err, "the store information")
	}
	if _, err := h.app.Settings.SetLogo(h.ctx, h.opts.Logo); err != nil {
		return h.wrap(err, "the logo")
	}
	h.res.Logo = true
	return nil
}

// catalogue creates every product in dollars, with its cost and its carton, and pins the everyday ones to the till.
func (h *home) catalogue() error {
	for _, item := range h.products {
		p, err := h.app.Catalog.Create(h.ctx, domain.Draft{NameAR: item.NameAR, NameEN: item.NameEN, Barcode: item.Barcode, UnitCode: item.Unit,
			PriceCurrency: "USD", Price: item.Price, Cost: item.Cost, UnitsPerCarton: item.UnitsPerCarton})
		if err != nil {
			return h.wrap(err, "the product "+item.NameEN)
		}
		if item.QuickSlot > 0 {
			if p, err = h.app.Catalog.SetQuickSlot(h.ctx, p.ID, item.QuickSlot); err != nil {
				return h.wrap(err, "a quick button")
			}
			h.res.QuickSlots++
		}
		h.byName[item.NameAR], h.kind[item.NameAR] = p, item.Kind
		h.res.Products++
	}
	return nil
}

// dollarsOnly goes over to dollars only before the first sale, through the same switch the owner uses — there is nothing
// to convert yet, so the plan is empty and the shop simply counts in dollars from its first day.
func (h *home) dollarsOnly() error {
	if err := h.owner(); err != nil {
		return err
	}
	plan, err := h.app.USDMode.Plan(h.ctx)
	if err != nil {
		return h.wrap(err, "the dollars-only plan")
	}
	_, err = h.app.USDMode.Apply(h.ctx, plan.Token)
	return h.wrap(err, "going over to dollars only")
}

// openings enters the stock on the shelves on the first morning, at each product's cost.
func (h *home) openings() error {
	if err := h.owner(); err != nil {
		return err
	}
	for _, item := range h.products {
		p := h.byName[item.NameAR]
		if _, err := h.app.Stock.Opening(h.ctx, stock.ReceiveInput{ProductID: p.ID, Quantity: item.Opening, Note: "جرد الافتتاح",
			Cost: stockdomain.CostInput{Mode: stockdomain.CostUnit, Amount: item.Cost, Currency: "USD"}}); err != nil {
			return h.wrap(err, "the opening stock of "+item.NameEN)
		}
		h.res.Openings++
	}
	return nil
}

// parties creates the suppliers and the customers, and brings over the balances of the paper book.
func (h *home) parties(d homeData) error {
	if err := h.owner(); err != nil {
		return err
	}
	for _, s := range d.Suppliers {
		created, err := h.app.Suppliers.Create(h.ctx, suppliersdomain.Draft{Name: s.Name, Phone: s.Phone, City: s.City})
		if err != nil {
			return h.wrap(err, "the supplier "+s.Key)
		}
		h.suppliers[s.Key] = created
		h.res.Suppliers++
		if s.Opening != "" {
			if _, err = h.app.Suppliers.Opening(h.ctx, suppliers.MoneyInput{SupplierID: created.ID, Currency: "USD", Amount: s.Opening, Note: s.OpeningNote}); err != nil {
				return h.wrap(err, "a supplier's opening balance")
			}
		}
	}
	for _, c := range d.Customers {
		created, err := h.app.Customers.Create(h.ctx, customersdomain.Draft{Name: c.Name, Phone: c.Phone, City: c.City, Note: c.Note})
		if err != nil {
			return h.wrap(err, "the customer "+c.Key)
		}
		h.customers[c.Key] = created
		h.res.Customers++
		if c.Opening != "" {
			if _, err = h.app.Customers.Opening(h.ctx, customers.AmountInput{CustomerID: created.ID, Currency: "USD", Amount: c.Opening,
				Note: "رصيد من الدفتر الورقي"}); err != nil {
				return h.wrap(err, "a customer's opening balance")
			}
			h.res.DebtOpenings++
		}
	}
	return nil
}

// storyLine is a product and a quantity, by name, as the story writes them.
type storyLine struct {
	name string
	qty  int
}

// cartOf turns the story's lines into a cart's, taking no more of a product than the shelf holds — the history's walk-in
// sales run first and may have sold it — and leaving out what is gone.
func (h *home) cartOf(lines []storyLine) ([]salesdomain.LineInput, error) {
	var out []salesdomain.LineInput
	for _, l := range lines {
		p, err := h.product(l.name)
		if err != nil {
			return nil, err
		}
		have, err := h.onHand(p)
		if err != nil {
			return nil, err
		}
		qty := min(int64(l.qty), have)
		if qty <= 0 {
			continue
		}
		out = append(out, salesdomain.LineInput{ProductID: p.ID, Quantity: strconv.FormatInt(qty, 10)})
	}
	return out, nil
}

// checkout quotes a cart and pays it with the quote's token — through the owner, when it carries a discount.
func (h *home) checkout(cart salesdomain.CartInput) (salesdomain.Sale, error) {
	q, err := h.app.Sales.Quote(h.ctx, cart)
	if err != nil {
		return salesdomain.Sale{}, h.wrap(err, "a quote")
	}
	if q.Discounted {
		if err = h.owner(); err != nil {
			return salesdomain.Sale{}, err
		}
	}
	sale, err := h.app.Sales.Checkout(h.ctx, sales.CheckoutInput{Cart: cart, Token: q.Token})
	return sale, h.wrap(err, "a checkout")
}

// credit is a sale on credit to a customer of the story, with what they paid there and then.
func (h *home) credit(customer string, paidNow string, lines ...storyLine) error {
	c, ok := h.customers[customer]
	if !ok {
		return errs.Internal(CodeDebtsInconsistent, "the home demo names a customer it did not create").WithParam("customer", customer)
	}
	cartLines, err := h.cartOf(lines)
	if err != nil || len(cartLines) == 0 {
		return err
	}
	if paidNow == "" {
		paidNow = "0"
	}
	if _, err = h.checkout(salesdomain.CartInput{Lines: cartLines, Payment: salesdomain.PaymentCredit, CustomerID: c.ID,
		Settlement: "USD", TenderCurrency: "USD", Tendered: paidNow}); err != nil {
		return err
	}
	h.res.CreditSales++
	return nil
}

// repay is a customer paying off part of their balance — or all of it.
func (h *home) repay(customer, amount string) error {
	c, ok := h.customers[customer]
	if !ok {
		return errs.Internal(CodeDebtsInconsistent, "the home demo names a customer it did not create").WithParam("customer", customer)
	}
	in := customersdomain.CashInput{Currency: "USD", TenderCurrency: "USD", Amount: amount, All: amount == ""}
	q, err := h.app.Customers.QuotePayment(h.ctx, c.ID, in)
	if err != nil {
		return h.wrap(err, "quoting a repayment")
	}
	if _, err = h.app.Customers.RecordPayment(h.ctx, customers.PaymentInput{CustomerID: c.ID, Cash: in, Token: q.Token}); err != nil {
		return h.wrap(err, "a repayment")
	}
	h.res.Payments++
	return nil
}

// bought is one line of a supplier's invoice.
type bought struct {
	name              string
	qty, damaged      int
	unitCost          string
	discountPercent   string
	discountAmountUSD string
}

// purchase records a supplier's invoice: on credit, with what was paid there and then and from where.
func (h *home) purchase(supplier, ref, invoiceDiscount, paidNow, paidFrom string, lines ...bought) error {
	if err := h.owner(); err != nil {
		return err
	}
	s, ok := h.suppliers[supplier]
	if !ok {
		return errs.Internal(CodeStockInconsistent, "the home demo names a supplier it did not create").WithParam("supplier", supplier)
	}
	in := suppliersdomain.Input{SupplierID: s.ID, Currency: "USD", SupplierRef: ref, InvoiceDiscount: invoiceDiscount, PaidNow: paidNow, PaidFrom: paidFrom}
	for _, b := range lines {
		p, err := h.product(b.name)
		if err != nil {
			return err
		}
		li := suppliersdomain.LineInput{ProductID: p.ID, Quantity: strconv.Itoa(b.qty), UnitCost: b.unitCost,
			DiscountPercent: b.discountPercent, DiscountAmount: b.discountAmountUSD}
		if b.damaged > 0 {
			li.Damaged = strconv.Itoa(b.damaged)
		}
		in.Lines = append(in.Lines, li)
	}
	if paidFrom == string(suppliersdomain.SourceDrawer) {
		// Paid from the drawer only with the cash in it; the rest stays on the supplier's book.
		cash, err := h.drawer()
		if err != nil {
			return err
		}
		want, err := cents(paidNow)
		if err != nil {
			return err
		}
		if want > cash {
			in.PaidFrom = string(suppliersdomain.SourceOwner)
		}
	}
	if _, err := h.app.Suppliers.RecordPurchase(h.ctx, in); err != nil {
		return h.wrap(err, "a purchase from "+supplier)
	}
	h.res.Purchases++
	return nil
}

// payBack pays a supplier, from the drawer when it holds enough and from the owner's own money otherwise.
func (h *home) payBack(supplier, amount string, fromDrawer bool) error {
	if err := h.owner(); err != nil {
		return err
	}
	source := suppliersdomain.SourceOwner
	if fromDrawer {
		cash, err := h.drawer()
		if err != nil {
			return err
		}
		want, err := cents(amount)
		if err != nil {
			return err
		}
		if want <= cash {
			source = suppliersdomain.SourceDrawer
		}
	}
	if _, err := h.app.Suppliers.Pay(h.ctx, suppliers.MoneyInput{SupplierID: h.suppliers[supplier].ID, Currency: "USD", Amount: amount,
		Source: string(source), Note: "دفعة على الحساب"}); err != nil {
		return h.wrap(err, "a payment to "+supplier)
	}
	h.res.SupplierPayments++
	return nil
}

// cents reads a dollar amount as the story writes it — "180", "557.20" — exactly, in cents.
func cents(amount string) (int64, error) {
	usd, err := money.NewCurrency("USD", 2, round.HalfUp)
	if err != nil {
		return 0, err
	}
	m, err := money.Parse(usd, amount)
	if err != nil {
		return 0, errs.Wrap(err, errs.CategoryInternal, CodeStockInconsistent, "a demo amount that is not an amount: "+amount)
	}
	return m.Minor(), nil
}

// drawer is the dollars the drawer should hold now, in cents.
func (h *home) drawer() (int64, error) {
	cash, err := h.app.Reports.ExpectedCash(h.ctx, h.app.Reports.Today(), "USD")
	return cash, h.wrap(err, "the drawer")
}

// expense is money spent on running the shop: from the drawer, or paid by the owner.
func (h *home) expense(amount, category string, fromDrawer bool, note string) error {
	if err := h.owner(); err != nil {
		return err
	}
	if fromDrawer {
		cash, err := h.drawer()
		if err != nil {
			return err
		}
		want, err := cents(amount)
		if err != nil {
			return err
		}
		fromDrawer = want <= cash
	}
	if _, err := h.app.Cashbook.Record(h.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindExpense, Currency: "USD", Amount: amount,
		Category: category, FromDrawer: fromDrawer, Note: note}); err != nil {
		return h.wrap(err, "an expense")
	}
	h.res.Expenses++
	return nil
}

// loss writes goods off: broken in the shop or on the way to a customer.
func (h *home) loss(name string, reason stockdomain.Reason, note string) error {
	if err := h.owner(); err != nil {
		return err
	}
	p, err := h.product(name)
	if err != nil {
		return err
	}
	have, err := h.onHand(p)
	if err != nil || have < 1 {
		return err // nothing left on the shelf to break: the story moves on
	}
	if _, err = h.app.Stock.Adjust(h.ctx, stock.AdjustInput{ProductID: p.ID, Direction: stock.DirectionOut, Quantity: "1", Reason: reason, Note: note}); err != nil {
		return h.wrap(err, "a write-off")
	}
	h.res.Losses++
	return nil
}

// shortfall counts a product one short of what the books say.
func (h *home) shortfall(name, note string) error {
	if err := h.owner(); err != nil {
		return err
	}
	p, err := h.product(name)
	if err != nil {
		return err
	}
	have, err := h.onHand(p)
	if err != nil || have < 2 {
		return err
	}
	_, err = h.app.Stock.Count(h.ctx, stock.CountInput{ProductID: p.ID, Counted: strconv.FormatInt(have-1, 10), Note: note})
	return h.wrap(err, "a count")
}

// pick chooses a product of a kind with enough on the shelf to sell, leaving the reserve the story's own sales need.
func (h *home) pick(kind string, qty int64) (domain.Product, bool, error) {
	reserve := map[string]int64{"big": 1, "mid": 2, "small": 6}[kind]
	var candidates []homeProduct
	for _, item := range h.products {
		if item.Kind == kind {
			candidates = append(candidates, item)
		}
	}
	for range 6 {
		item := candidates[h.rng.Intn(len(candidates))]
		p := h.byName[item.NameAR]
		have, err := h.onHand(p)
		if err != nil {
			return domain.Product{}, false, err
		}
		if have >= qty+reserve {
			return p, true, nil
		}
	}
	return domain.Product{}, false, nil
}

// walkIn is a customer off the street: a few small things, sometimes a piece, now and then furniture — paid in cash, often
// with a note larger than the total, and a sofa's price sometimes rounded down by the owner.
func (h *home) walkIn(big bool) error {
	cart := salesdomain.CartInput{Settlement: "USD"}
	seen := map[string]bool{}
	add := func(kind string, qty int64) error {
		p, ok, err := h.pick(kind, qty)
		if err != nil || !ok || seen[p.NameAR] {
			return err
		}
		seen[p.NameAR] = true
		cart.Lines = append(cart.Lines, salesdomain.LineInput{ProductID: p.ID, Quantity: strconv.FormatInt(qty, 10)})
		return nil
	}
	switch {
	case big:
		if err := add("big", 1); err != nil {
			return err
		}
		if h.rng.Intn(2) == 0 {
			if err := add("mid", 1); err != nil {
				return err
			}
		}
	case h.rng.Intn(4) == 0:
		if err := add("mid", 1+int64(h.rng.Intn(2))); err != nil {
			return err
		}
	}
	for range h.rng.Intn(4) {
		qty := int64(1 + h.rng.Intn(3))
		if err := add("small", qty); err != nil {
			return err
		}
	}
	if len(cart.Lines) == 0 {
		return nil
	}
	q, err := h.app.Sales.Quote(h.ctx, cart)
	if err != nil {
		return h.wrap(err, "a walk-in quote")
	}
	switch {
	case big && h.rng.Intn(3) == 0:
		// The owner rounds a furniture sale down — "خصم للزبون" — with the PIN.
		cart.SaleDiscount = strconv.FormatInt(q.TotalMinor%5_000/100+10, 10)
	case q.TotalMinor >= 1_000 && h.rng.Intn(3) == 0:
		// Paid with the next ten or fifty dollars up, and given change.
		step := int64(1_000)
		if q.TotalMinor >= 20_000 {
			step = 5_000
		}
		cart.TenderCurrency, cart.Tendered = "USD", strconv.FormatInt((q.TotalMinor/step+1)*step/100, 10)
	}
	sale, err := h.checkout(cart)
	if err != nil {
		return err
	}
	h.sold = append(h.sold, sale)
	h.res.HistorySales++
	return nil
}

// closing counts the drawer at the end of the day — after the owner has taken the takings home on the days they do,
// leaving a float of a hundred dollars.
func (h *home) closing(bank, short bool) error {
	if err := h.owner(); err != nil {
		return err
	}
	cash, err := h.drawer()
	if err != nil {
		return err
	}
	if bank && cash > 15_000 {
		take := cash - 10_000
		take -= take % 1_000
		if _, err = h.app.Cashbook.Record(h.ctx, cashbook.RecordInput{Kind: cashbookdomain.KindWithdrawal, Currency: "USD",
			Amount: fmt.Sprintf("%d.%02d", take/100, take%100), Note: "إلى الخزنة"}); err != nil {
			return h.wrap(err, "the takings taken home")
		}
		if cash, err = h.drawer(); err != nil {
			return err
		}
	}
	counted := cash
	if short && counted >= 500 {
		counted -= 500
	}
	if _, err = h.app.Cashbook.Count(h.ctx, cashbook.CountInput{Currency: "USD", Counted: fmt.Sprintf("%d.%02d", counted/100, counted%100),
		Note: "عدّ آخر النهار"}); err != nil {
		return h.wrap(err, "the closing count")
	}
	h.res.Counts++
	return nil
}

// story is what happens on a day of the history beyond the walk-ins: the suppliers, the customers who buy on credit and
// pay it off, the expenses and the breakages. Days are counted from the first morning.
func (h *home) story(day int) error {
	type act func() error
	events := map[int][]act{
		0: {func() error { return h.expense("600", "rent", false, "إيجار الصالة — الشهر") }},
		1: {func() error {
			return h.purchase("shahba", "SH-1187", "", "500", "owner",
				bought{name: "طقم كنب ٣+٢+١", qty: 1, unitCost: "890.00", discountPercent: "5"},
				bought{name: "كنبة زاوية مودرن", qty: 2, unitCost: "600.00", discountPercent: "5"},
				bought{name: "سرير مزدوج ١٦٠×٢٠٠", qty: 2, unitCost: "290.00", discountPercent: "5"})
		}},
		2: {func() error {
			return h.credit("groom", "500", storyLine{"غرفة نوم كاملة", 1}, storyLine{"فرشة طبية ١٦٠×٢٠٠", 1}, storyLine{"طقم شراشف سرير مزدوج", 2}, storyLine{"مخدة نوم", 4})
		}},
		3: {func() error {
			return h.purchase("nour", "N-5520", "", "150", "owner",
				bought{name: "طقم طناجر ستانلس ١٠ قطع", qty: 6, unitCost: "62.00", discountPercent: "3"},
				bought{name: "طقم صحون بورسلان ٢٤ قطعة", qty: 6, unitCost: "45.00"},
				bought{name: "طقم كاسات زجاج ٦ قطع", qty: 24, damaged: 2, unitCost: "5.40"},
				bought{name: "طقم فناجين قهوة", qty: 24, unitCost: "7.50"},
				bought{name: "مقلاة غرانيت ٢٨ سم", qty: 12, unitCost: "9.80"})
		}},
		4: {func() error {
			return h.credit("hotel", "", storyLine{"كرسي سفرة منجّد", 12}, storyLine{"ستائر مخمل (زوج)", 10}, storyLine{"طقم شراشف سرير مزدوج", 10}, storyLine{"مخدة نوم", 24})
		}},
		5: {func() error {
			return h.credit("restaurant", "", storyLine{"طقم طناجر ستانلس ١٠ قطع", 2}, storyLine{"طقم صحون بورسلان ٢٤ قطعة", 4}, storyLine{"طقم كاسات زجاج ٦ قطع", 12},
				storyLine{"طقم سكاكين مع قاعدة", 6}, storyLine{"صينية تقديم", 12})
		}},
		6: {func() error {
			return h.credit("office", "200", storyLine{"طاولة مكتب", 3}, storyLine{"كرسي مكتب دوّار", 4})
		}, func() error { return h.expense("180", "wages", true, "أجور العمال — الأسبوع") }},
		8: {func() error {
			return h.purchase("furat", "F-331", "25", "", "",
				bought{name: "سجادة ٢×٣ م", qty: 6, unitCost: "78.00"},
				bought{name: "ستائر مخمل (زوج)", qty: 12, unitCost: "28.00"},
				bought{name: "بطانية شتوية", qty: 12, unitCost: "20.00"},
				bought{name: "طقم شراشف سرير مزدوج", qty: 12, unitCost: "17.00"})
		}},
		9: {func() error {
			return h.loss("مزهرية سيراميك", stockdomain.ReasonDamaged, "انكسرت أثناء ترتيب الرفوف")
		}},
		10: {func() error { return h.repay("groom", "300") }},
		11: {func() error {
			return h.credit("kindergarten", "100", storyLine{"طاولة وكراسي أطفال", 4}, storyLine{"خدادية ديكور", 10})
		}},
		13: {func() error { return h.payBack("shahba", "1000", false) }, func() error { return h.expense("180", "wages", true, "أجور العمال — الأسبوع") }},
		14: {func() error { return h.repay("hotel", "400") }, func() error { return h.expense("38", "electricity", true, "فاتورة الكهرباء") }},
		15: {func() error {
			// Paid in full, from the owner's own money: 557.20 dollars.
			return h.purchase("decor", "D-208", "", "557.20", "owner",
				bought{name: "مرآة جدارية مذهّبة", qty: 4, unitCost: "42.00"},
				bought{name: "مزهرية سيراميك", qty: 12, unitCost: "8.00"},
				bought{name: "ساعة حائط", qty: 10, unitCost: "10.00"},
				bought{name: "إطار صور", qty: 48, unitCost: "2.40"},
				bought{name: "نبتة صناعية مع أصيص", qty: 6, unitCost: "13.00"})
		}},
		16: {func() error {
			return h.credit("reem", "150", storyLine{"تسريحة مع مرآة", 1}, storyLine{"كومودينو", 2})
		}},
		17: {func() error {
			return h.purchase("arz", "AR-77", "", "", "",
				bought{name: "ثريا كريستال", qty: 4, unitCost: "118.00", discountPercent: "3"},
				bought{name: "أباجورة", qty: 12, unitCost: "16.00", discountPercent: "3"},
				bought{name: "لمبة ليد ١٢ واط", qty: 100, unitCost: "0.80"})
		}},
		19: {func() error { return h.repay("restaurant", "150") }, func() error {
			return h.loss("طقم كاسات زجاج ٦ قطع", stockdomain.ReasonDamaged, "كسر أثناء التوصيل")
		}},
		20: {func() error { return h.repay("abumohammad", "80") }, func() error { return h.expense("180", "wages", true, "أجور العمال — الأسبوع") },
			func() error { return h.expense("15", "supplies", true, "أكياس وكرتون تغليف") }},
		21: {func() error {
			return h.credit("charity", "300", storyLine{"بطانية شتوية", 12}, storyLine{"مخدة نوم", 24}, storyLine{"فرشة طبية ١٦٠×٢٠٠", 2})
		}},
		22: {h.returnSomething},
		23: {func() error {
			return h.purchase("nour", "N-5604", "", "", "",
				bought{name: "طقم كاسات زجاج ٦ قطع", qty: 24, damaged: 1, unitCost: "5.40"},
				bought{name: "علب حفظ طعام ٥ قطع", qty: 24, unitCost: "4.40"},
				bought{name: "ترمس شاي وقهوة", qty: 12, unitCost: "6.50"},
				bought{name: "غلاية كهربائية", qty: 6, unitCost: "12.00"})
		}},
		24: {func() error { return h.repay("office", "250") }},
		25: {func() error { return h.repay("groom", "300") }},
		26: {func() error { return h.shortfall("إطار صور", "جرد قسم الديكور") }},
		27: {func() error {
			return h.purchase("shahba", "SH-1240", "", "800", "owner",
				bought{name: "غرفة نوم كاملة", qty: 1, unitCost: "1180.00", discountPercent: "8"},
				bought{name: "خزانة ملابس ٤ أبواب", qty: 2, unitCost: "360.00", discountPercent: "8"},
				bought{name: "طاولة سفرة مع ٦ كراسي", qty: 2, unitCost: "540.00", discountPercent: "8"})
		}, func() error { return h.expense("180", "wages", true, "أجور العمال — الأسبوع") }},
		28: {func() error {
			return h.credit("hotel", "", storyLine{"سجادة ٢×٣ م", 4}, storyLine{"ثريا كريستال", 2}, storyLine{"مرآة جدارية مذهّبة", 2})
		}},
		29: {func() error { return h.payBack("furat", "400", true) }, func() error { return h.expense("38", "electricity", true, "فاتورة الكهرباء") }},
		30: {func() error {
			return h.loss("فرشة طبية ١٦٠×٢٠٠", stockdomain.ReasonDamaged, "تلف بسبب تسرّب مياه في المستودع")
		},
			func() error { return h.expense("600", "rent", false, "إيجار الصالة — الشهر") }},
		31: {func() error { return h.payBack("arz", "250", true) }},
		33: {func() error { return h.repay("reem", "") }},
		34: {func() error {
			return h.credit("umkhaled", "10", storyLine{"ترمس شاي وقهوة", 1}, storyLine{"علب حفظ طعام ٥ قطع", 2}, storyLine{"طقم فناجين قهوة", 1})
		},
			func() error { return h.expense("180", "wages", true, "أجور العمال — الأسبوع") }},
		35: {func() error { return h.repay("kindergarten", "120") }},
		36: {func() error { return h.payBack("nour", "300", false) }},
		37: {func() error { return h.credit("samer", "", storyLine{"كرسي استرخاء هزاز", 1}) }},
		38: {func() error { return h.repay("groom", "300") }},
		39: {func() error {
			return h.purchase("decor", "D-251", "", "150", "drawer",
				bought{name: "لوحة جدارية كانفاس", qty: 6, unitCost: "21.00"},
				bought{name: "شمعدان نحاسي", qty: 12, unitCost: "7.00"},
				bought{name: "مزهرية سيراميك", qty: 12, unitCost: "8.00"})
		}},
		40: {func() error { return h.repay("hotel", "600") }},
		41: {func() error { return h.repay("samer", "") }, func() error { return h.expense("180", "wages", true, "أجور العمال — الأسبوع") }},
		42: {func() error { return h.repay("charity", "200") }},
		43: {func() error {
			return h.credit("restaurant", "", storyLine{"مقلاة غرانيت ٢٨ سم", 4}, storyLine{"طنجرة ضغط ٧ لتر", 2}, storyLine{"صينية تقديم", 4})
		}},
		44: {func() error { return h.payBack("shahba", "1200", false) }, func() error { return h.expense("22", "transport", true, "أجور توصيل غرفة نوم") }},
	}
	for _, e := range events[day] {
		if err := e(); err != nil {
			return err
		}
	}
	return nil
}

// day runs one day of the history: the story's acts in the morning, walk-ins through the day, a void on day twelve, the
// drawer counted at closing — five dollars short once, on day eighteen.
func (h *home) day(day int) error {
	h.sold = nil
	h.at(9, 30)
	if err := h.story(day); err != nil {
		return err
	}
	friday := h.morning.Weekday() == time.Friday
	visitors := 3 + h.rng.Intn(6)
	if friday {
		visitors = 1 + h.rng.Intn(3)
	}
	bigToday := h.rng.Intn(100) < 40
	for i := range visitors {
		h.at(10+i*9/max(visitors, 1), h.rng.Intn(60))
		if err := h.walkIn(bigToday && i == visitors/2); err != nil {
			return err
		}
	}
	if day == 12 && len(h.sold) > 0 {
		h.at(19, 0)
		if err := h.owner(); err != nil {
			return err
		}
		if _, err := h.app.Sales.Void(h.ctx, sales.VoidInput{SaleID: h.sold[len(h.sold)-1].ID, Reason: "فاتورة مكرّرة بالخطأ"}); err != nil {
			return h.wrap(err, "a void")
		}
		h.res.HistoryVoids++
	}
	h.at(20, 0)
	if friday {
		return nil
	}
	return h.closing(day%3 == 2, day == 18)
}

// returnSomething takes back one item of a recent walk-in sale — "the colour did not suit" — refunded in cash and put back
// on the shelf.
func (h *home) returnSomething() error {
	for back := 1; back <= 3; back++ {
		date := h.morning.AddDate(0, 0, -back).Format("2006-01-02")
		d, err := h.app.Sales.Day(h.ctx, date)
		if err != nil {
			return err
		}
		for _, s := range d.Sales {
			if s.Status != salesdomain.StatusPosted || s.Payment != salesdomain.PaymentCash {
				continue
			}
			for _, l := range s.Lines {
				if h.kind[l.NameAR] != "small" && h.kind[l.NameAR] != "mid" {
					continue
				}
				if err = h.owner(); err != nil {
					return err
				}
				draft := salesdomain.ReturnDraft{Settlement: salesdomain.SettleCash, Reason: "لم يناسب اللون",
					Lines: []salesdomain.ReturnLineDraft{{SaleLineID: l.ID, Quantity: "1", Restock: true}}}
				if _, err = h.app.Sales.Return(h.ctx, sales.ReturnInput{SaleID: s.ID, Draft: draft}); err != nil {
					return h.wrap(err, "a return")
				}
				h.res.Returns++
				return nil
			}
		}
	}
	return nil
}

// opensAfter is how long after nine the shop has been open before today has anything in it.
const opensAfter = 30 * time.Minute

// today is the day so far: a few customers and one paying something off, spread between opening and now. Seeded before the
// shop has opened, today has nothing in it — nothing is ever dated after the moment the seeder ran.
func (h *home) today(now time.Time) error {
	h.sold = nil
	elapsed := now.Sub(h.morning)
	if elapsed < opensAfter {
		return nil
	}
	steps := []func() error{
		func() error { return h.walkIn(false) },
		func() error { return h.repay("abuali", "40") },
		func() error { return h.walkIn(true) },
		func() error { return h.walkIn(false) },
		func() error { return h.walkIn(false) },
	}
	for i, step := range steps {
		h.opts.Clock.Current = h.morning.Add(elapsed * time.Duration(i+1) / time.Duration(len(steps)+1))
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// backup takes the shop's first backup, so its Backups screen has one. No outside folder is set: a folder named here would
// not exist on the computer the demo is restored on.
func (h *home) backup() error {
	if err := h.owner(); err != nil {
		return err
	}
	if _, err := h.app.Safety.TakeNow(h.ctx); err != nil {
		return h.wrap(err, "the first backup")
	}
	list, err := h.app.Safety.List(h.ctx)
	h.res.Backups = len(list)
	if err != nil {
		return err
	}
	_, err = h.app.Owner.EndElevation(h.ctx)
	return err
}

// Export writes a verified copy of the seeded shop to path, for another computer to bring in from Backups → Restore from a
// file (0.10.1): a Windows till the demo is shown on. It takes a backup of the shop as it now stands and saves a copy of it,
// the owner's act the Backups screen's "Save a copy" is — so the file is one the application wrote and checked, not a copy
// of a database that was open.
func Export(ctx context.Context, app *bootstrap.App, pin, path string) error {
	if _, err := app.Owner.Elevate(ctx, pin); err != nil {
		return err
	}
	taken, err := app.Safety.TakeNow(ctx)
	if err != nil {
		return err
	}
	if err = app.Safety.SaveCopy(ctx, taken.Name, path); err != nil {
		return err
	}
	_, err = app.Owner.EndElevation(ctx)
	return err
}
