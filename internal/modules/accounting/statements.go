package accounting

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
)

// Stable codes for the statements.
const (
	CodeInvalidRange = "accounting.invalid_range"
)

// The permissions guarding the statements.
//
// VIEW permissions, all of them, and that is not decoration: nothing in this file writes, so
// there is no state a wider grant could damage. What it protects is CONFIDENTIALITY — a till
// operator has no business reading the company's margins.
const (
	PermProfitAndLossView = "accounting.profit_and_loss.view"
	PermBalanceSheetView  = "accounting.balance_sheet.view"
)

// StatementNode is one line of a statement, with its children beneath it.
//
// # Presentation-signed, unlike everything else the ledger returns
//
// The ledger is debit-positive throughout, so revenue reads negative and a reader who is not an
// accountant does not trust the report. This is the presentation edge 0012 named — the one place
// the convention is converted — so `AmountMinor` here is POSITIVE when the account holds what it
// normally holds. Revenue of 40,000 reads 40,000.
type StatementNode struct {
	AccountID   id.ID
	Code        string
	Name        string
	Type        domain.AccountType
	Depth       int
	IsPostable  bool
	AmountMinor int64
	Children    []StatementNode
}

// ProfitAndLoss is what happened between two dates.
type ProfitAndLoss struct {
	// Requested is the range asked for; Covered is what whole fiscal periods could answer. They
	// differ when a request lands mid-period, and a caller that shows one without the other is
	// reporting a month's figures under a fortnight's heading.
	RequestedFrom string
	RequestedTo   string
	Covered       sqlite.CoveredRange

	Revenue []StatementNode
	Expense []StatementNode

	RevenueMinor int64
	ExpenseMinor int64

	// NetProfitMinor is revenue less expense: the LEDGER's answer, which includes rent, wages and
	// every other cost no sale knows about.
	//
	// It is named in full, never "profit", because Phase 8's analysis found the word to be the
	// trap: gross margin is a different number computed by a different module, and a screen
	// showing one labelled "profit" is how a shopkeeper concludes they are doing well while
	// losing money.
	NetProfitMinor int64
}

// BalanceSheet is the position as at a date.
type BalanceSheet struct {
	AsAt    string
	Covered sqlite.CoveredRange

	Asset     []StatementNode
	Liability []StatementNode
	Equity    []StatementNode

	AssetMinor     int64
	LiabilityMinor int64
	// EquityMinor INCLUDES ResultMinor. A reader wants one equity figure that ties, not two they
	// have to add up themselves.
	EquityMinor int64

	// ResultMinor is the profit not yet closed into retained earnings.
	//
	// # Why a balance sheet must carry it
	//
	// Assets equal liabilities plus equity only once revenue and expense have been closed out.
	// Until the year end, the profit so far sits in accounts the balance sheet does not show, and
	// a sheet that ignored them would be out of balance by exactly the profit — every day of
	// every year except one.
	//
	// It is computed here rather than stored, because closing has not happened. A stored figure
	// would be a second home for something the revenue and expense accounts already say.
	ResultMinor int64

	// OutOfBalanceMinor is assets less liabilities and equity. Zero is the only acceptable value.
	//
	// REPORTED rather than enforced, which is 7.4 D3's rule: a report that refuses to render when
	// the books are wrong hides the evidence needed to find out why. §20.5's verifier is what
	// fixes it; this is what makes anyone look.
	OutOfBalanceMinor int64
}

// ProfitAndLoss reports revenue and expense over a range.
func (s *Service) ProfitAndLoss(
	ctx context.Context, companyID id.ID, from, to string,
) (ProfitAndLoss, error) {
	if err := checkRange(from, to); err != nil {
		return ProfitAndLoss{}, err
	}

	positions, covered, err := s.repos.PositionsInRange(ctx, companyID, from, to)
	if err != nil {
		return ProfitAndLoss{}, err
	}

	statement := ProfitAndLoss{RequestedFrom: from, RequestedTo: to, Covered: covered}
	statement.Revenue, statement.RevenueMinor = treeOf(positions, domain.Revenue)
	statement.Expense, statement.ExpenseMinor = treeOf(positions, domain.Expense)
	statement.NetProfitMinor = statement.RevenueMinor - statement.ExpenseMinor
	return statement, nil
}

// BalanceSheet reports the position as at a date.
func (s *Service) BalanceSheet(
	ctx context.Context, companyID id.ID, asAt string,
) (BalanceSheet, error) {
	if err := checkRange(asAt, asAt); err != nil {
		return BalanceSheet{}, err
	}

	positions, covered, err := s.repos.PositionsAsAt(ctx, companyID, asAt)
	if err != nil {
		return BalanceSheet{}, err
	}

	sheet := BalanceSheet{AsAt: asAt, Covered: covered}
	sheet.Asset, sheet.AssetMinor = treeOf(positions, domain.Asset)
	sheet.Liability, sheet.LiabilityMinor = treeOf(positions, domain.Liability)
	sheet.Equity, sheet.EquityMinor = treeOf(positions, domain.Equity)

	_, revenue := treeOf(positions, domain.Revenue)
	_, expense := treeOf(positions, domain.Expense)
	sheet.ResultMinor = revenue - expense
	sheet.EquityMinor += sheet.ResultMinor

	sheet.OutOfBalanceMinor = sheet.AssetMinor - sheet.LiabilityMinor - sheet.EquityMinor
	return sheet, nil
}

// checkRange refuses a range that cannot mean anything.
//
// Dates are ISO-8601 strings throughout this codebase, so string comparison IS date comparison —
// which is the reason that format was chosen (§E) and not a coincidence being relied on.
func checkRange(from, to string) error {
	if from == "" || to == "" {
		return errs.Validation(CodeInvalidRange, "a statement needs both ends of its range")
	}
	if from > to {
		return errs.Validation(CodeInvalidRange, "a statement's range ends before it starts").
			WithParam("from", from).WithParam("to", to)
	}
	return nil
}

// treeOf builds one section of a statement and returns its total.
//
// # The subtotal is computed, never read
//
// A parent account's figure is the sum of its children, and the ledger holds a figure for the
// parent too — postings to a non-postable account are refused, so that figure is zero, but
// nothing in the schema promises it. Summing the CHILDREN means a stray posting to a parent
// cannot inflate a subtotal invisibly; it would surface as a leaf that does not tie.
//
// The total returned is the sum of the ROOTS, for the same reason: adding every node would count
// every leaf twice, once as itself and once inside its parent.
func treeOf(
	positions []sqlite.AccountPosition, kind domain.AccountType,
) ([]StatementNode, int64) {
	var (
		children = make(map[id.ID][]sqlite.AccountPosition, len(positions))
		roots    = make([]sqlite.AccountPosition, 0, 8)
		inKind   = make(map[id.ID]bool, len(positions))
	)
	for _, position := range positions {
		if position.Type == kind {
			inKind[position.AccountID] = true
		}
	}
	for _, position := range positions {
		if position.Type != kind {
			continue
		}
		// A parent of a different TYPE is not a parent for this section. Chart-of-accounts
		// designs put revenue and expense under one "profit and loss" heading often enough that
		// treating such a node as a root here is what keeps the section self-contained.
		if position.ParentID.IsZero() || !inKind[position.ParentID] {
			roots = append(roots, position)
			continue
		}
		children[position.ParentID] = append(children[position.ParentID], position)
	}

	nodes := make([]StatementNode, 0, len(roots))
	var total int64
	for _, root := range roots {
		node := buildNode(root, children)
		total += node.AmountMinor
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Code < nodes[j].Code })
	return nodes, total
}

func buildNode(
	position sqlite.AccountPosition, children map[id.ID][]sqlite.AccountPosition,
) StatementNode {
	node := StatementNode{
		AccountID: position.AccountID, Code: position.Code, Name: position.Name,
		Type: position.Type, Depth: position.Depth, IsPostable: position.IsPostable,
		AmountMinor: presentationSign(position),
	}

	kids := children[position.AccountID]
	if len(kids) == 0 {
		return node
	}

	node.Children = make([]StatementNode, 0, len(kids))
	var sum int64
	for _, kid := range kids {
		child := buildNode(kid, children)
		sum += child.AmountMinor
		node.Children = append(node.Children, child)
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return node.Children[i].Code < node.Children[j].Code
	})
	node.AmountMinor = sum
	return node
}

// presentationSign flips a debit-positive figure to read positive in its own direction.
//
// A credit-normal account — revenue, a liability, equity — holds a NEGATIVE debit-positive
// balance when it holds what it should. Negating it is the whole of the conversion, and doing it
// here means every caller gets the same answer rather than each deciding.
func presentationSign(position sqlite.AccountPosition) int64 {
	if position.Normal == domain.Credit {
		return -position.Amount
	}
	return position.Amount
}
