package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	customersdomain "github.com/mizan-erp/mizan/internal/lite/customers/domain"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	salesdomain "github.com/mizan-erp/mizan/internal/lite/sales/domain"
)

func TestTheDebtBookThroughTheBindings(t *testing.T) {
	set, oil := tillShop(t) // olive oil at $3.25, 15,000

	created := set.Customers.Create(api.CustomerInput{Name: " أبو محمد ", Phone: "٠٩٣٣ ١٢٣", Note: "الحلاق"})
	if !created.OK || created.Data.Name != "أبو محمد" || created.Data.Phone != "0933 123" || len(created.Data.Balances) != 0 {
		t.Fatalf("Create = %+v", created)
	}
	abu := created.Data
	if r := set.Customers.Create(api.CustomerInput{Name: "ابو محمد"}); codeOf(t, r) != customersdomain.CodeDuplicateName || r.Error.Params["existingName"] != "أبو محمد" {
		t.Fatalf("a duplicate = %+v", r)
	}

	// On credit, charged in dollars: 4 L = $13.00, 45,000 pounds ($3.00) paid now, $10.00 added.
	cart := api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "4"}}, Settlement: "USD", TenderCurrency: "SYP",
		Tendered: "45000", Payment: "credit", CustomerID: abu.ID}
	q := quoted(t, set, cart)
	if q.Payment != "credit" || q.NeedsCustomer || q.CustomerName != "أبو محمد" || q.Debt != "10.00" || q.BalanceBefore != "0.00" || q.BalanceAfter != "10.00" || q.Change != "0.00" {
		t.Fatalf("credit quote = %+v", q)
	}
	sold := set.Till.Checkout(api.CheckoutInput{Cart: cart, Token: q.Token})
	if !sold.OK || sold.Data.Payment != "credit" || sold.Data.CreditCustomerName != "أبو محمد" || sold.Data.CreditAmount != "10.00" ||
		sold.Data.CreditBalanceAfter != "10.00" || sold.Data.CreditCurrency != "USD" || sold.Data.CreditReversed {
		t.Fatalf("credit checkout = %+v", sold)
	}
	if r := set.Till.Quote(api.CartInput{Lines: cart.Lines, Payment: "credit", CustomerID: "nope"}); codeOf(t, r) != salesdomain.CodeCustomerNotFound {
		t.Fatal("a malformed customer id")
	}
	if day := set.Sales.List(""); !day.OK || day.Data.Totals[1].OnCredit != "10.00" {
		t.Fatalf("the day's credit = %+v", day.Data.Totals)
	}

	// Who owes what: per currency, with the other currency as a labelled reference.
	out := set.Customers.Outstanding()
	if !out.OK || len(out.Data.Customers) != 1 || out.Data.Rate != "15000" || out.Data.LocalCurrency != "SYP" {
		t.Fatalf("Outstanding = %+v", out)
	}
	bal := out.Data.Customers[0].Balances
	if len(bal) != 1 || bal[0] != (api.BalanceDTO{Currency: "USD", Balance: "10.00", OwedSince: "2026-09-14", Reference: "150000", ReferenceCurrency: "SYP"}) {
		t.Fatalf("balances = %+v", bal)
	}
	if found := set.Customers.Search(api.CustomerQueryDTO{Text: "٠٩٣٣", OwingOnly: true}); !found.OK || len(found.Data) != 1 {
		t.Fatalf("Search by phone = %+v", found)
	}

	// Pay all in pounds: $10.00 at 15,000 is 150,000 exactly. A stale quote is refused.
	pay := api.PaymentInput{CustomerID: abu.ID, Currency: "USD", TenderCurrency: "SYP", All: true}
	pq := set.Customers.QuotePayment(pay)
	if !pq.OK || pq.Data.Tendered != "150000" || pq.Data.Settled != "10.00" || pq.Data.BalanceAfter != "0.00" || pq.Data.Rate != "15000" {
		t.Fatalf("QuotePayment = %+v", pq)
	}
	stale := pay
	stale.Token = "old"
	if r := set.Customers.RecordPayment(stale); codeOf(t, r) != customersdomain.CodePaymentStale {
		t.Fatal("a payment with a stale token")
	}
	pay.Token = pq.Data.Token
	paid := set.Customers.RecordPayment(pay)
	if !paid.OK || paid.Data.Kind != "payment" || paid.Data.Amount != "-10.00" || paid.Data.Tendered != "150000" || paid.Data.BalanceAfter != "0.00" || !paid.Data.Reversible {
		t.Fatalf("RecordPayment = %+v", paid)
	}

	// The owner's acts refuse outside owner mode.
	if r := set.Customers.Opening(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "50000", Note: "صفحة 3"}); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("an opening outside owner mode")
	}
	if r := set.Customers.Reverse(api.ReverseEntryInput{EntryID: paid.Data.ID, Reason: "x"}); codeOf(t, r) != ownerdomain.CodeRequired {
		t.Fatal("a reversal outside owner mode")
	}
	elevate(t, set)
	opening := set.Customers.Opening(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "50000", Note: "صفحة 3"})
	if !opening.OK || opening.Data.Amount != "50000" || opening.Data.Note != "صفحة 3" {
		t.Fatalf("Opening = %+v", opening)
	}
	if r := set.Customers.WriteOff(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", All: true}); codeOf(t, r) != customersdomain.CodeReasonRequired {
		t.Fatal("a write-off without a reason")
	}
	if w := set.Customers.WriteOff(api.DebtAmountInput{CustomerID: abu.ID, Currency: "SYP", Amount: "10000", Note: "خصم"}); !w.OK || w.Data.BalanceAfter != "40000" {
		t.Fatalf("WriteOff = %+v", w)
	}

	// Voiding the repaid credit sale leaves the shop owing $10; refunded all in pounds.
	if r := set.Sales.Void(api.VoidInput{SaleID: sold.Data.ID, Reason: "أعاد الزيت"}); !r.OK || !r.Data.CreditReversed {
		t.Fatalf("Void = %+v", r)
	}
	st := set.Customers.Statement(api.StatementQueryDTO{CustomerID: abu.ID, Currency: "USD"})
	if !st.OK || st.Data.Balance != "-10.00" || len(st.Data.Entries) != 3 || st.Data.Entries[0].Kind != "reversal" || st.Data.Entries[2].Kind != "charge" ||
		st.Data.Entries[2].Reversible || !st.Data.Entries[2].Reversed || st.Data.Entries[2].SaleID != sold.Data.ID || st.Data.Rate != "15000" {
		t.Fatalf("Statement = %+v", st)
	}
	refund := set.Customers.Refund(api.RefundInput{CustomerID: abu.ID, Currency: "USD", TenderCurrency: "SYP", All: true, Reason: "نقداً"})
	if !refund.OK || refund.Data.Tendered != "150000" || refund.Data.BalanceAfter != "0.00" {
		t.Fatalf("Refund = %+v", refund)
	}
	if r := set.Customers.Reverse(api.ReverseEntryInput{EntryID: "nope", Reason: "x"}); codeOf(t, r) != customersdomain.CodeEntryNotFound {
		t.Fatal("reversing a malformed id")
	}
	if off := set.Customers.SetActive(api.SetCustomerActiveInput{ID: abu.ID, RowVersion: abu.RowVersion, Active: false}); !off.OK || off.Data.Active {
		t.Fatalf("SetActive = %+v", off)
	}
	if edit := set.Customers.Update(api.UpdateCustomerInput{ID: abu.ID, RowVersion: abu.RowVersion, Name: "x"}); codeOf(t, edit) != "database.concurrent_modification" {
		t.Fatal("a stale edit")
	}
	if r := set.Customers.Statement(api.StatementQueryDTO{CustomerID: "nope", Currency: "USD"}); codeOf(t, r) != customersdomain.CodeNotFound {
		t.Fatal("a statement for a malformed id")
	}
}

// debtDTOs are every DTO the debt book crosses the boundary in.
var debtDTOs = []reflect.Type{
	reflect.TypeFor[api.BalanceDTO](), reflect.TypeFor[api.CustomerDTO](), reflect.TypeFor[api.EntryDTO](), reflect.TypeFor[api.StatementDTO](),
	reflect.TypeFor[api.DebtDayDTO](), reflect.TypeFor[api.OutstandingDTO](), reflect.TypeFor[api.PaymentQuoteDTO](),
}

// TestNoDTOCarriesABalanceSummedAcrossCurrencies holds L5 H2 at the boundary: nothing named a total, and every figure a
// string (DESIGN D9). The integers allowed are counts, places and versions.
func TestNoDTOCarriesABalanceSummedAcrossCurrencies(t *testing.T) {
	allowed := map[string]bool{"Seq": true, "RowVersion": true, "Payments": true, "Refunds": true}
	for _, typ := range debtDTOs {
		for i := range typ.NumField() {
			f := typ.Field(i)
			name := strings.ToLower(f.Name)
			for _, forbidden := range []string{"total", "sum", "combined", "overall"} {
				if strings.Contains(name, forbidden) {
					t.Errorf("%s.%s looks like a figure across currencies", typ.Name(), f.Name)
				}
			}
			switch f.Type.Kind() {
			case reflect.String, reflect.Bool, reflect.Slice, reflect.Struct:
			case reflect.Int, reflect.Int64:
				if !allowed[f.Name] {
					t.Errorf("%s.%s crosses as a number", typ.Name(), f.Name)
				}
			default:
				t.Errorf("%s.%s is a %s", typ.Name(), f.Name, f.Type.Kind())
			}
		}
	}
}
