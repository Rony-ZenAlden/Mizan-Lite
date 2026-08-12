package sales

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// AllocateNumberForTest exposes the allocator to this module's tests.
//
// In an _test.go file so it is not part of the shipped API. `allocateNumber` is unexported for a
// structural reason — no caller outside this module can take a number without a document to
// attach it to — and a test-only export keeps that true while still letting the allocation be
// tested on its own, including under contention.
//
// It opens its own transaction, which is what the real caller (posting, in 5.3) will already be
// inside.
func (s *Service) AllocateNumberForTest(
	ctx context.Context, branchID id.ID, code string,
) (string, error) {
	var number string
	err := s.db.Do(ctx, func(txCtx context.Context) error {
		var allocErr error
		number, allocErr = s.allocateNumber(txCtx, branchID, code)
		return allocErr
	})
	return number, err
}
