package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/expenses/domain"
)

// ExpenseCategoryDTO is one kind of spending.
type ExpenseCategoryDTO struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
	// NameKey is set for a SHIPPED category, so the screen renders it through the catalog like
	// everything else. Empty for one a customer added, whose name is their own words.
	NameKey string `json:"nameKey"`
	// TaxRecoverable drives the one thing an operator must be told before they enter a claim:
	// whether the tax on this can be reclaimed.
	TaxRecoverable bool `json:"taxRecoverable"`
}

// ExpenseDTO is one expense.
type ExpenseDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Number string `json:"number"`

	PayeeName   string `json:"payeeName"`
	ExpenseDate string `json:"expenseDate"`
	Reference   string `json:"reference"`
	Description string `json:"description"`

	Settlement string `json:"settlement"`
	PaidMethod string `json:"paidMethod"`
	DueDate    string `json:"dueDate"`

	Currency   string `json:"currency"`
	NetMinor   string `json:"netMinor"`
	TaxMinor   string `json:"taxMinor"`
	TotalMinor string `json:"totalMinor"`
	// OutstandingMinor is what is still owed. Blank on an expense paid when it was recorded —
	// absent rather than zero, because "0.00" reads as a debt that has been settled.
	OutstandingMinor string `json:"outstandingMinor"`

	IsTemplate bool `json:"isTemplate"`
}

// ExpenseLineDTO is one category and amount on an expense.
type ExpenseLineDTO struct {
	ID           string `json:"id"`
	LineNumber   int    `json:"lineNumber"`
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	Description  string `json:"description"`

	NetMinor       string `json:"netMinor"`
	TaxAmountMinor string `json:"taxAmountMinor"`
	TaxRecoverable bool   `json:"taxRecoverable"`
	TotalMinor     string `json:"totalMinor"`
}

// ExpenseDetailDTO is an expense with its lines.
type ExpenseDetailDTO struct {
	Expense  ExpenseDTO       `json:"expense"`
	Lines    []ExpenseLineDTO `json:"lines"`
	Editable bool             `json:"editable"`
}

// DebtDTO is money in or out with no trade document.
type DebtDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Number string `json:"number"`

	Direction        string `json:"direction"`
	Kind             string `json:"kind"`
	CounterpartyName string `json:"counterpartyName"`
	DebtDate         string `json:"debtDate"`
	DueDate          string `json:"dueDate"`
	Method           string `json:"method"`
	Reference        string `json:"reference"`

	Currency    string `json:"currency"`
	AmountMinor string `json:"amountMinor"`
}

// DebtPositionDTO is the net position of one kind of obligation.
type DebtPositionDTO struct {
	Kind string `json:"kind"`
	// NetMinor is SIGNED: positive means money came in on balance. This is the one place a sign
	// belongs — the documents keep positive amounts and a direction.
	NetMinor string `json:"netMinor"`
}

// Expenses is the money-out surface.
type Expenses struct{ graph }

// expensePolicies declares what each method requires.
//
// Recording an expense and PAYING one are separate grants, and recording a DEBT is a third.
// Money moving with no trade document behind it is the shape every misappropriation takes, and
// "the owner drew 40,000" is a sentence somebody should have had to be authorised to write.
func expensePolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Categories":    policy.Requires(expenses.PermExpenseView),
		"Expenses":      policy.Requires(expenses.PermExpenseView),
		"Expense":       policy.Requires(expenses.PermExpenseView),
		"Unsettled":     policy.Requires(expenses.PermExpenseView),
		"Draft":         policy.Requires(expenses.PermExpenseDraft),
		"AddLine":       policy.Requires(expenses.PermExpenseDraft),
		"RemoveLine":    policy.Requires(expenses.PermExpenseDraft),
		"Record":        policy.Requires(expenses.PermExpensePost),
		"Cancel":        policy.Requires(expenses.PermExpenseDraft),
		"Settle":        policy.Requires(expenses.PermSettlePost),
		"Debts":         policy.Requires(expenses.PermDebtView),
		"DebtPositions": policy.Requires(expenses.PermDebtView),
		"RecordDebt":    policy.Requires(expenses.PermDebtRecord),
	}
}

// Categories lists a company's expense categories.
func (e *Expenses) Categories() envelope.Result[[]ExpenseCategoryDTO] {
	ctx, app, err := e.guard("Categories")
	if err != nil {
		return envelope.Fail[[]ExpenseCategoryDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]ExpenseCategoryDTO](err)
	}

	categories, err := app.Expenses.Categories(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]ExpenseCategoryDTO](err)
	}

	out := make([]ExpenseCategoryDTO, 0, len(categories))
	for _, category := range categories {
		if !category.IsActive {
			continue
		}
		out = append(out, ExpenseCategoryDTO{
			ID: string(category.ID), Code: category.Code, Name: category.Name,
			NameKey: category.NameKey, TaxRecoverable: category.TaxRecoverable,
		})
	}
	return envelope.Ok(out)
}

// Expenses lists a company's expenses.
func (e *Expenses) Expenses(status string) envelope.Result[[]ExpenseDTO] {
	ctx, app, err := e.guard("Expenses")
	if err != nil {
		return envelope.Fail[[]ExpenseDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]ExpenseDTO](err)
	}

	records, err := app.Expenses.Expenses(ctx, companyID, domain.Status(status))
	if err != nil {
		return envelope.Fail[[]ExpenseDTO](err)
	}

	out := make([]ExpenseDTO, 0, len(records))
	for _, record := range records {
		row := expenseRow(record)
		if record.Status == domain.Posted && record.Settlement == domain.OnAccount {
			outstanding, oweErr := app.Expenses.OutstandingOn(ctx, record.ID)
			if oweErr != nil {
				return envelope.Fail[[]ExpenseDTO](oweErr)
			}
			row.OutstandingMinor = minor(outstanding)
		}
		out = append(out, row)
	}
	return envelope.Ok(out)
}

// Expense reads one expense with its lines.
func (e *Expenses) Expense(expenseID string) envelope.Result[ExpenseDetailDTO] {
	ctx, app, err := e.guard("Expense")
	if err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}
	record, lines, err := app.Expenses.Expense(ctx, id.ID(expenseID))
	if err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}

	row := expenseRow(record)
	if record.Status == domain.Posted && record.Settlement == domain.OnAccount {
		outstanding, oweErr := app.Expenses.OutstandingOn(ctx, record.ID)
		if oweErr != nil {
			return envelope.Fail[ExpenseDetailDTO](oweErr)
		}
		row.OutstandingMinor = minor(outstanding)
	}

	out := ExpenseDetailDTO{
		Expense: row, Lines: make([]ExpenseLineDTO, 0, len(lines)),
		Editable: record.Status == domain.Draft,
	}
	for _, line := range lines {
		out.Lines = append(out.Lines, ExpenseLineDTO{
			ID: string(line.ID), LineNumber: line.LineNumber,
			CategoryID: string(line.CategoryID), CategoryName: line.CategoryName,
			Description:    line.Description,
			NetMinor:       minor(line.NetMinor),
			TaxAmountMinor: minor(line.TaxAmountMinor),
			TaxRecoverable: line.TaxRecoverable,
			TotalMinor:     minor(line.TotalMinor),
		})
	}
	return envelope.Ok(out)
}

// Unsettled lists what is still owed, oldest due first.
func (e *Expenses) Unsettled() envelope.Result[[]ExpenseDTO] {
	ctx, app, err := e.guard("Unsettled")
	if err != nil {
		return envelope.Fail[[]ExpenseDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]ExpenseDTO](err)
	}

	records, err := app.Expenses.Unsettled(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]ExpenseDTO](err)
	}

	out := make([]ExpenseDTO, 0, len(records))
	for _, record := range records {
		row := expenseRow(record)
		outstanding, oweErr := app.Expenses.OutstandingOn(ctx, record.ID)
		if oweErr != nil {
			return envelope.Fail[[]ExpenseDTO](oweErr)
		}
		row.OutstandingMinor = minor(outstanding)
		out = append(out, row)
	}
	return envelope.Ok(out)
}

// NewExpenseInput opens an expense.
type NewExpenseInput struct {
	PartnerID   string `json:"partnerId"`
	PayeeName   string `json:"payeeName"`
	ExpenseDate string `json:"expenseDate"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
	// Settlement is "immediate" or "on_account". PaidMethod is required for the first and must
	// be empty for the second — an expense on account has not been paid, so naming a method
	// would assert something that has not happened.
	Settlement string `json:"settlement"`
	PaidMethod string `json:"paidMethod"`
	DueDate    string `json:"dueDate"`
	Currency   string `json:"currency"`
}

// Draft opens an expense.
func (e *Expenses) Draft(in NewExpenseInput) envelope.Result[ExpenseDTO] {
	ctx, app, err := e.guard("Draft")
	if err != nil {
		return envelope.Fail[ExpenseDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[ExpenseDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[ExpenseDTO](err)
	}

	record, err := app.Expenses.Draft(ctx, expenses.NewExpenseInput{
		CompanyID: companyID, BranchID: branchID,
		PartnerID: id.ID(in.PartnerID), PayeeName: in.PayeeName,
		ExpenseDate: in.ExpenseDate, Reference: in.Reference, Description: in.Description,
		Settlement: domain.Settlement(in.Settlement),
		PaidMethod: domain.Method(in.PaidMethod),
		DueDate:    in.DueDate, Currency: in.Currency,
	})
	if err != nil {
		return envelope.Fail[ExpenseDTO](err)
	}
	return envelope.Ok(expenseRow(record))
}

// AddExpenseLineInput puts a category and an amount on an expense.
type AddExpenseLineInput struct {
	ExpenseID   string `json:"expenseId"`
	CategoryID  string `json:"categoryId"`
	Description string `json:"description"`
	NetMinor    string `json:"netMinor"`
}

// AddLine puts a category and an amount on a draft expense.
func (e *Expenses) AddLine(in AddExpenseLineInput) envelope.Result[ExpenseDetailDTO] {
	ctx, app, err := e.guard("AddLine")
	if err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}
	amount, err := parseScaled(in.NetMinor)
	if err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}

	if _, err = app.Expenses.AddLine(ctx, expenses.AddLineInput{
		ExpenseID: id.ID(in.ExpenseID), CategoryID: id.ID(in.CategoryID),
		Description: in.Description, NetMinor: amount,
	}); err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}
	return e.Expense(in.ExpenseID)
}

// RemoveLine takes a line off a draft.
func (e *Expenses) RemoveLine(expenseID, lineID string) envelope.Result[ExpenseDetailDTO] {
	ctx, app, err := e.guard("RemoveLine")
	if err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}
	if err = app.Expenses.RemoveLine(ctx, id.ID(expenseID), id.ID(lineID)); err != nil {
		return envelope.Fail[ExpenseDetailDTO](err)
	}
	return e.Expense(expenseID)
}

// Record posts an expense.
func (e *Expenses) Record(expenseID string) envelope.Result[ExpenseDTO] {
	ctx, app, err := e.guard("Record")
	if err != nil {
		return envelope.Fail[ExpenseDTO](err)
	}
	record, err := app.Expenses.Record(ctx, id.ID(expenseID))
	if err != nil {
		return envelope.Fail[ExpenseDTO](err)
	}
	return envelope.Ok(expenseRow(record))
}

// Cancel abandons a draft expense.
func (e *Expenses) Cancel(expenseID string) envelope.Result[bool] {
	ctx, app, err := e.guard("Cancel")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	if err = app.Expenses.Cancel(ctx, id.ID(expenseID)); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// SettleInput pays what an expense left owed.
type SettleInput struct {
	PartnerID   string `json:"partnerId"`
	PayeeName   string `json:"payeeName"`
	PaymentDate string `json:"paymentDate"`
	Method      string `json:"method"`
	Reference   string `json:"reference"`
	Currency    string `json:"currency"`
	AmountMinor string `json:"amountMinor"`
	// ExpenseID is optional: money against a running balance is real, and forcing an allocation
	// would mean inventing one against an expense not yet entered.
	ExpenseID string `json:"expenseId"`
}

// Settle pays what an expense left owed.
func (e *Expenses) Settle(in SettleInput) envelope.Result[bool] {
	ctx, app, err := e.guard("Settle")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[bool](err)
	}
	amount, err := parseScaled(in.AmountMinor)
	if err != nil {
		return envelope.Fail[bool](err)
	}

	settle := []expenses.Allocation{}
	if in.ExpenseID != "" {
		settle = append(settle, expenses.Allocation{
			ExpenseID: id.ID(in.ExpenseID), AmountMinor: amount,
		})
	}

	if _, err = app.Expenses.Settle(ctx, expenses.SettleInput{
		CompanyID: companyID, BranchID: branchID,
		PartnerID: id.ID(in.PartnerID), PayeeName: in.PayeeName,
		PaymentDate: in.PaymentDate, Method: domain.Method(in.Method),
		Reference: in.Reference, Currency: in.Currency, AmountMinor: amount,
		Settle: settle,
	}); err != nil {
		return envelope.Fail[bool](err)
	}
	return envelope.Ok(true)
}

// Debts lists money in and out with no trade document.
func (e *Expenses) Debts(kind string) envelope.Result[[]DebtDTO] {
	ctx, app, err := e.guard("Debts")
	if err != nil {
		return envelope.Fail[[]DebtDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]DebtDTO](err)
	}

	records, err := app.Expenses.Debts(ctx, companyID, domain.Kind(kind))
	if err != nil {
		return envelope.Fail[[]DebtDTO](err)
	}

	out := make([]DebtDTO, 0, len(records))
	for _, record := range records {
		out = append(out, DebtDTO{
			ID: string(record.ID), Status: string(record.Status), Number: record.Number,
			Direction: string(record.Direction), Kind: string(record.Kind),
			CounterpartyName: record.CounterpartyName, DebtDate: record.DebtDate,
			DueDate: record.DueDate, Method: string(record.Method),
			Reference: record.Reference, Currency: record.CurrencyCode,
			AmountMinor: minor(record.AmountMinor),
		})
	}
	return envelope.Ok(out)
}

// DebtPositions reports the net position of each kind of obligation.
func (e *Expenses) DebtPositions() envelope.Result[[]DebtPositionDTO] {
	ctx, app, err := e.guard("DebtPositions")
	if err != nil {
		return envelope.Fail[[]DebtPositionDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]DebtPositionDTO](err)
	}

	positions, err := app.Expenses.DebtPositions(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]DebtPositionDTO](err)
	}

	// Every kind, in a fixed order, whether or not it has moved. A screen showing only the kinds
	// with activity would silently change shape as a business used it, and "the owner has taken
	// nothing out" is an answer somebody wants to see stated.
	out := make([]DebtPositionDTO, 0, 3)
	for _, kind := range []domain.Kind{
		domain.LoanPayable, domain.LoanReceivable, domain.OwnerEquity,
	} {
		out = append(out, DebtPositionDTO{
			Kind: string(kind), NetMinor: minor(positions[kind]),
		})
	}
	return envelope.Ok(out)
}

// NewDebtInput records money in or out with no trade document.
type NewDebtInput struct {
	Direction        string `json:"direction"`
	Kind             string `json:"kind"`
	PartnerID        string `json:"partnerId"`
	CounterpartyName string `json:"counterpartyName"`
	DebtDate         string `json:"debtDate"`
	DueDate          string `json:"dueDate"`
	Method           string `json:"method"`
	Reference        string `json:"reference"`
	Description      string `json:"description"`
	Currency         string `json:"currency"`
	AmountMinor      string `json:"amountMinor"`
}

// RecordDebt records money in or out with no trade document.
func (e *Expenses) RecordDebt(in NewDebtInput) envelope.Result[DebtDTO] {
	ctx, app, err := e.guard("RecordDebt")
	if err != nil {
		return envelope.Fail[DebtDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[DebtDTO](err)
	}
	branchID, err := currentBranch(ctx, app)
	if err != nil {
		return envelope.Fail[DebtDTO](err)
	}
	amount, err := parseScaled(in.AmountMinor)
	if err != nil {
		return envelope.Fail[DebtDTO](err)
	}

	record, err := app.Expenses.RecordDebt(ctx, expenses.NewDebtInput{
		CompanyID: companyID, BranchID: branchID,
		Direction: domain.Direction(in.Direction), Kind: domain.Kind(in.Kind),
		PartnerID: id.ID(in.PartnerID), CounterpartyName: in.CounterpartyName,
		DebtDate: in.DebtDate, DueDate: in.DueDate, Method: domain.Method(in.Method),
		Reference: in.Reference, Description: in.Description,
		Currency: in.Currency, AmountMinor: amount,
	})
	if err != nil {
		return envelope.Fail[DebtDTO](err)
	}
	return envelope.Ok(DebtDTO{
		ID: string(record.ID), Status: string(record.Status), Number: record.Number,
		Direction: string(record.Direction), Kind: string(record.Kind),
		CounterpartyName: record.CounterpartyName, DebtDate: record.DebtDate,
		Method: string(record.Method), Currency: record.CurrencyCode,
		AmountMinor: minor(record.AmountMinor),
	})
}

func expenseRow(e domain.Expense) ExpenseDTO {
	return ExpenseDTO{
		ID: string(e.ID), Status: string(e.Status), Number: e.Number,
		PayeeName: e.PayeeName, ExpenseDate: e.ExpenseDate,
		Reference: e.Reference, Description: e.Description,
		Settlement: string(e.Settlement), PaidMethod: string(e.PaidMethod),
		DueDate: e.DueDate, Currency: e.CurrencyCode,
		NetMinor: minor(e.NetMinor), TaxMinor: minor(e.TaxMinor),
		TotalMinor: minor(e.TotalMinor), IsTemplate: e.IsTemplate,
	}
}
