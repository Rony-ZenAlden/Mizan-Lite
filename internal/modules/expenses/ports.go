package expenses

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Tax computes what an expense line is taxed.
//
// Narrower than the tax service behind it, for the reason every port here is: this module needs
// "what is the tax on this amount", not the engine's whole surface.
type Tax interface {
	TaxFor(ctx context.Context, q TaxQuery) (ResolvedTax, error)
}

// TaxQuery asks what a line is taxed.
type TaxQuery struct {
	CompanyID id.ID
	PartnerID id.ID
	NetMinor  int64
	Date      string
}

// ResolvedTax is the tax and the rate that produced it.
type ResolvedTax struct {
	AmountMinor int64
	RateMicro   int64
	Code        string
}

// Numbering allocates a document number, from the platform allocator every transactional module
// shares (6.1).
type Numbering interface {
	Allocate(ctx context.Context, branchID id.ID, seriesCode string) (string, error)
}
