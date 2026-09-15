// Package printing records what was printed and numbers debt vouchers (L7 §5, §7). Rendering and sending belong to the
// documents and printers packages; this module owns the print_jobs and voucher_numbers tables, so a reprint knows it is a copy
// and a voucher keeps its number.
package printing

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes.
const (
	CodeUnknownKind = "lite.printing.unknown_kind"
)

// Kind is what was printed.
type Kind string

// The kinds.
const (
	KindSale       Kind = "sale"
	KindCreditSale Kind = "credit_sale"
	KindPayment    Kind = "payment"
	KindRefund     Kind = "refund"
	KindTest       Kind = "test"
)

// Job is one print job as recorded.
type Job struct {
	ID        id.ID
	Seq       int64
	Kind      Kind
	SubjectID id.ID
	CopyNo    int
	Printer   string
	Path      string
	Sent      bool
	ErrorCode string
	PrintedAt time.Time
}

// Store persists jobs and voucher numbers. infra/sqlite and printingtest.Fake implement it; one contract runs against both.
type Store interface {
	NextSeq(ctx context.Context) (int64, error)
	Insert(ctx context.Context, j Job) error
	// SentCopies counts the jobs of a subject the queue took.
	SentCopies(ctx context.Context, subjectID id.ID) (int, error)
	Jobs(ctx context.Context, limit int) ([]Job, error)
	// AssignVoucher gives an entry the next voucher number; an entry numbered already keeps its number.
	AssignVoucher(ctx context.Context, entryID id.ID) (int64, error)
	Voucher(ctx context.Context, entryID id.ID) (int64, bool, error)
}

// Transactor runs fn atomically.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service records print jobs and numbers vouchers.
type Service struct {
	tx    Transactor
	store Store
	clk   clock.Clock
	newID func() (id.ID, error)
}

// NewService builds the service.
func NewService(tx Transactor, store Store, clk clock.Clock) *Service {
	return &Service{tx: tx, store: store, clk: clk, newID: id.New}
}

// NextCopy is the copy number the next print of a subject is: 1 for the first the queue takes, 2 and on for copies.
func (s *Service) NextCopy(ctx context.Context, subjectID id.ID) (int, error) {
	if subjectID == "" {
		return 1, nil
	}
	n, err := s.store.SentCopies(ctx, subjectID)
	return n + 1, err
}

// Record writes a job the printers package sent, or failed to.
func (s *Service) Record(ctx context.Context, j Job) (Job, error) {
	switch j.Kind {
	case KindSale, KindCreditSale, KindPayment, KindRefund, KindTest:
	default:
		return Job{}, errs.Validation(CodeUnknownKind, "unknown print job").WithParam("value", string(j.Kind))
	}
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		if j.ID, err = s.newID(); err != nil {
			return err
		}
		if j.Seq, err = s.store.NextSeq(ctx); err != nil {
			return err
		}
		j.PrintedAt = s.clk.Now().UTC().Truncate(time.Millisecond)
		if j.Sent {
			j.ErrorCode = ""
		}
		return s.store.Insert(ctx, j)
	})
	return j, err
}

// Jobs lists the newest jobs.
func (s *Service) Jobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	return s.store.Jobs(ctx, limit)
}

// AssignVoucher numbers a debt payment or refund inside its caller's transaction (Q-L7.3).
func (s *Service) AssignVoucher(ctx context.Context, entryID id.ID) (int64, error) {
	var n int64
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		n, err = s.store.AssignVoucher(ctx, entryID)
		return err
	})
	return n, err
}

// Voucher is an entry's voucher number; found is false for an entry recorded before L7.
func (s *Service) Voucher(ctx context.Context, entryID id.ID) (int64, bool, error) {
	return s.store.Voucher(ctx, entryID)
}
