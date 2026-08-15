package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	expensesdomain "github.com/mizan-erp/mizan/internal/modules/expenses/domain"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/purchasing"
	purchasingdomain "github.com/mizan-erp/mizan/internal/modules/purchasing/domain"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	salesdomain "github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// The adapters that give the partner module a balance without letting it import anything.
//
// A partner's position spans sales, purchasing and expenses. No one module owns it and none may
// import another, so each contributes through a port satisfied here — which is what the
// composition root is for.

// partnerReceivables is what customers owe, from sales.
type partnerReceivables struct{ sales *sales.Service }

var _ partner.Receivables = partnerReceivables{}

func (r partnerReceivables) OpenFor(
	ctx context.Context, companyID, partnerID id.ID,
) ([]partner.OpenItem, error) {
	documents, err := r.sales.Documents(ctx, companyID, salesdomain.Invoice, salesdomain.Posted)
	if err != nil {
		return nil, err
	}

	out := make([]partner.OpenItem, 0, 8)
	for _, document := range documents {
		if document.PartnerID != partnerID {
			continue
		}
		outstanding, oweErr := r.sales.Outstanding(ctx, document.ID)
		if oweErr != nil {
			return nil, oweErr
		}
		// A settled invoice is not an OPEN item. Including it would put a paid document on a
		// statement, which is the fastest way to have a customer stop reading them.
		if outstanding == 0 {
			continue
		}
		out = append(out, partner.OpenItem{
			DocumentType: "sales.invoice", DocumentID: document.ID,
			DocumentNumber: document.Number, Date: document.Date, DueDate: document.DueDate,
			TotalMinor: document.TotalMinor, SettledMinor: document.TotalMinor - outstanding,
			OutstandingMinor: outstanding,
		})
	}
	return out, nil
}

// partnerPayables is what the business owes: supplier bills and expenses left on account.
type partnerPayables struct {
	purchasing *purchasing.Service
	expenses   *expenses.Service
}

var _ partner.Payables = partnerPayables{}

func (p partnerPayables) OpenFor(
	ctx context.Context, companyID, partnerID id.ID,
) ([]partner.OpenItem, error) {
	out := make([]partner.OpenItem, 0, 8)

	bills, err := p.purchasing.Bills(ctx, companyID, purchasingdomain.BillPosted)
	if err != nil {
		return nil, err
	}
	for _, bill := range bills {
		if bill.PartnerID != partnerID {
			continue
		}
		outstanding, oweErr := p.purchasing.OutstandingOnBill(ctx, bill.ID)
		if oweErr != nil {
			return nil, oweErr
		}
		if outstanding == 0 {
			continue
		}
		out = append(out, partner.OpenItem{
			DocumentType: "purchasing.bill", DocumentID: bill.ID,
			DocumentNumber: bill.Number, Date: bill.BillDate, DueDate: bill.DueDate,
			TotalMinor: bill.TotalMinor, SettledMinor: bill.TotalMinor - outstanding,
			OutstandingMinor: outstanding,
		})
	}

	// Expenses recorded on account are owed to somebody too, and a landlord who is also a
	// supplier should see one balance rather than two.
	unsettled, err := p.expenses.Unsettled(ctx, companyID)
	if err != nil {
		return nil, err
	}
	for _, expense := range unsettled {
		if expense.PartnerID != partnerID {
			continue
		}
		outstanding, oweErr := p.expenses.OutstandingOn(ctx, expense.ID)
		if oweErr != nil {
			return nil, oweErr
		}
		if outstanding == 0 {
			continue
		}
		out = append(out, partner.OpenItem{
			DocumentType: "expenses.expense", DocumentID: expense.ID,
			DocumentNumber: expense.Number, Date: expense.ExpenseDate,
			DueDate:    expense.DueDate,
			TotalMinor: expense.TotalMinor, SettledMinor: expense.TotalMinor - outstanding,
			OutstandingMinor: outstanding,
		})
	}
	return out, nil
}

var _ = expensesdomain.OnAccount

// partnerControl is what the GENERAL LEDGER says, from accounting.
//
// The other side of invariant 5. The subsidiary ledger and the control account are computed by
// completely different paths — one sums documents and allocations, the other sums journal lines
// written by posting rules — and when they disagree, one of them is wrong in a way nothing else
// will surface.
type partnerControl struct{ accounting *accounting.Service }

var _ partner.ControlLedger = partnerControl{}

func (c partnerControl) ControlBalances(
	ctx context.Context, companyID, partnerID id.ID,
) (receivableMinor, payableMinor int64, err error) {
	receivableMinor, err = c.accounting.PartnerControlBalance(ctx, companyID, partnerID, "AR")
	if err != nil {
		return 0, 0, err
	}
	payable, err := c.accounting.PartnerControlBalance(ctx, companyID, partnerID, "AP")
	if err != nil {
		return 0, 0, err
	}
	// Payables are a CREDIT balance in a debit-positive world, so the ledger returns them
	// negative. The partner module thinks in "what is owed", which is positive — flipping it
	// here rather than there keeps the sign convention where the accounts are.
	return receivableMinor, -payable, nil
}
