package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
)

// AccountDTO is one line of the chart of accounts.
//
// `depth` and `isPostable` cross so the screen can indent the hierarchy and grey out headings
// without re-deriving either — the same reasoning that stores normal_balance rather than
// computing it in five places.
type AccountDTO struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	NameKey  string `json:"nameKey"`
	Type     string `json:"type"`
	Normal   string `json:"normal"`
	Depth    int    `json:"depth"`
	Postable bool   `json:"isPostable"`
	System   bool   `json:"isSystem"`
	Active   bool   `json:"isActive"`
}

// TrialBalanceRowDTO is one account's position for a period.
//
// Amounts cross as STRINGS of minor units, not as numbers. JavaScript's number type is a
// float64 and loses integer precision above 2^53 — which a currency with no decimal places
// reaches at around nine quadrillion, far enough away to feel safe and near enough that a
// hyperinflated currency gets there. Money crosses as text and is formatted, never arithmetic'd,
// on the frontend.
type TrialBalanceRowDTO struct {
	AccountID string `json:"accountId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Normal    string `json:"normal"`

	OpeningMinor string `json:"openingMinor"`
	DebitMinor   string `json:"debitMinor"`
	CreditMinor  string `json:"creditMinor"`
	ClosingMinor string `json:"closingMinor"`
}

// TrialBalanceDTO is the report, with the totals that prove it balances.
type TrialBalanceDTO struct {
	PeriodID string               `json:"periodId"`
	Rows     []TrialBalanceRowDTO `json:"rows"`

	TotalDebitMinor  string `json:"totalDebitMinor"`
	TotalCreditMinor string `json:"totalCreditMinor"`
	// Balanced is the assertion made visible. Under the debit-positive convention a correct
	// trial balance sums to zero; showing the flag rather than making the reader add up a
	// hundred rows is the whole point of putting it on screen.
	Balanced bool `json:"balanced"`
}

// FiscalPeriodDTO is one period, for the picker and the close screen.
type FiscalPeriodDTO struct {
	ID       string `json:"id"`
	Sequence int    `json:"sequence"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Status   string `json:"status"`
}

// Accounting is the read-only ledger surface (§20.6, tier v1.1).
//
// # Why read-only
//
// §20.6 exposes accounting over five releases: v1.0 shows nothing while keeping full
// double-entry books, v1.1 adds the chart and the trial balance READ-ONLY, and manual journal
// entries do not arrive until v1.3. This binding is the v1.1 tier, and it deliberately offers
// no way to post, edit, or close anything — those screens come with the releases that
// introduce them, and shipping their bindings early would put buttons behind a permission
// nobody has yet.
type Accounting struct{ graph }

// accountingPolicies declares what each method requires.
func accountingPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Chart":        policy.Requires(accounting.PermAccountView),
		"TrialBalance": policy.Requires(accounting.PermAccountView),
		"Periods":      policy.Requires(accounting.PermAccountView),
	}
}

// Chart lists the company's accounts, in hierarchy order.
func (a *Accounting) Chart() envelope.Result[[]AccountDTO] {
	ctx, app, err := a.guard("Chart")
	if err != nil {
		return envelope.Fail[[]AccountDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]AccountDTO](err)
	}
	accounts, err := app.Accounting.Accounts(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]AccountDTO](err)
	}

	out := make([]AccountDTO, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, AccountDTO{
			ID: string(account.ID), Code: account.Code, Name: account.Name,
			NameKey: account.NameKey, Type: string(account.Type), Normal: string(account.Normal),
			Depth: account.Depth, Postable: account.IsPostable,
			System: account.IsSystem, Active: account.IsActive,
		})
	}
	return envelope.Ok(out)
}

// Periods lists the current financial year's periods.
func (a *Accounting) Periods() envelope.Result[[]FiscalPeriodDTO] {
	ctx, app, err := a.guard("Periods")
	if err != nil {
		return envelope.Fail[[]FiscalPeriodDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]FiscalPeriodDTO](err)
	}
	years, err := app.Accounting.Years(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]FiscalPeriodDTO](err)
	}
	if len(years) == 0 {
		return envelope.Ok([]FiscalPeriodDTO{})
	}

	periods, err := app.Accounting.Periods(ctx, years[0].ID)
	if err != nil {
		return envelope.Fail[[]FiscalPeriodDTO](err)
	}
	out := make([]FiscalPeriodDTO, 0, len(periods))
	for _, period := range periods {
		out = append(out, FiscalPeriodDTO{
			ID: string(period.ID), Sequence: period.Sequence,
			Start:  period.Start.Format("2006-01-02"),
			End:    period.End.Format("2006-01-02"),
			Status: period.Status,
		})
	}
	return envelope.Ok(out)
}

// TrialBalance reads every account's position for a period.
func (a *Accounting) TrialBalance(periodID string) envelope.Result[TrialBalanceDTO] {
	ctx, app, err := a.guard("TrialBalance")
	if err != nil {
		return envelope.Fail[TrialBalanceDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[TrialBalanceDTO](err)
	}
	balances, err := app.Accounting.TrialBalance(ctx, companyID, id.ID(periodID))
	if err != nil {
		return envelope.Fail[TrialBalanceDTO](err)
	}

	dto := TrialBalanceDTO{PeriodID: periodID, Rows: make([]TrialBalanceRowDTO, 0, len(balances))}
	var debits, credits, closing int64
	for _, balance := range balances {
		debits += balance.Debit
		credits += balance.Credit
		closing += balance.Closing
		dto.Rows = append(dto.Rows, TrialBalanceRowDTO{
			AccountID: string(balance.AccountID), Code: balance.AccountCode,
			Name: balance.AccountName, Type: string(balance.AccountType),
			Normal:       string(balance.Normal),
			OpeningMinor: minor(balance.Opening), DebitMinor: minor(balance.Debit),
			CreditMinor: minor(balance.Credit), ClosingMinor: minor(balance.Closing),
		})
	}
	dto.TotalDebitMinor = minor(debits)
	dto.TotalCreditMinor = minor(credits)
	// The debit-positive sum, which is zero when the books balance (2.3).
	dto.Balanced = closing == 0 && debits == credits
	return envelope.Ok(dto)
}

// minor renders an integer minor-unit amount as text for the boundary.
func minor(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
