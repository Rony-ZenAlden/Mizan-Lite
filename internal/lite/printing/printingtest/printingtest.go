// Package printingtest is the in-memory printing store and the contract every store must meet.
package printingtest

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/printing"
)

// Fake is an in-memory printing.Store.
type Fake struct {
	mu       sync.Mutex
	jobs     []printing.Job
	vouchers map[id.ID]int64
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{vouchers: map[id.ID]int64{}} }

func (f *Fake) NextSeq(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.jobs)) + 1, nil
}

func (f *Fake) Insert(_ context.Context, j printing.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.jobs {
		if o.Seq == j.Seq || o.ID == j.ID {
			return errs.Conflict("database.duplicate", "unique")
		}
	}
	if (j.Kind == printing.KindTest) != (j.SubjectID == "") || j.CopyNo < 1 || j.Printer == "" || (!j.Sent && j.ErrorCode == "") {
		return errs.Validation("database.constraint_violation", "check")
	}
	f.jobs = append(f.jobs, j)
	return nil
}

func (f *Fake) SentCopies(_ context.Context, subjectID id.ID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, j := range f.jobs {
		if j.SubjectID == subjectID && j.Sent {
			n++
		}
	}
	return n, nil
}

func (f *Fake) Jobs(_ context.Context, limit int) ([]printing.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]printing.Job(nil), f.jobs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *Fake) AssignVoucher(_ context.Context, entryID id.ID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.vouchers[entryID]; ok {
		return n, nil
	}
	n := int64(len(f.vouchers)) + 1
	f.vouchers[entryID] = n
	return n, nil
}

func (f *Fake) Voucher(_ context.Context, entryID id.ID) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.vouchers[entryID]
	return n, ok, nil
}

// Subject is a fresh store and a way to make a debt entry a voucher may number.
type Subject struct {
	Store    printing.Store
	NewEntry func(t *testing.T) id.ID
}

// StoreContract is the behaviour every printing.Store must have.
func StoreContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	t.Run("jobs read back newest first, copies count only what the queue took", func(t *testing.T) {
		sub := newSubject(t)
		sale, _ := id.New()
		put := func(j printing.Job) {
			t.Helper()
			var err error
			j.ID, _ = id.New()
			if j.Seq, err = sub.Store.NextSeq(ctx); err != nil {
				t.Fatal(err)
			}
			j.PrintedAt = at
			if err = sub.Store.Insert(ctx, j); err != nil {
				t.Fatal(err)
			}
		}
		put(printing.Job{Kind: printing.KindSale, SubjectID: sale, CopyNo: 1, Printer: "Xprinter", Path: "driver", Sent: true})
		put(printing.Job{Kind: printing.KindSale, SubjectID: sale, CopyNo: 2, Printer: "Xprinter", Path: "raw", ErrorCode: "lite.printers.send_failed"})
		put(printing.Job{Kind: printing.KindSale, SubjectID: sale, CopyNo: 2, Printer: "Xprinter", Path: "raw", Sent: true})
		put(printing.Job{Kind: printing.KindTest, CopyNo: 1, Printer: "Xprinter", Path: "raw", Sent: true})
		if n, _ := sub.Store.SentCopies(ctx, sale); n != 2 {
			t.Fatalf("sent copies %d", n)
		}
		jobs, err := sub.Store.Jobs(ctx, 3)
		if err != nil || len(jobs) != 3 || jobs[0].Kind != printing.KindTest || jobs[2].ErrorCode != "lite.printers.send_failed" || jobs[2].Sent ||
			!jobs[1].Sent || jobs[1].CopyNo != 2 || !jobs[0].PrintedAt.Equal(at) {
			t.Fatalf("jobs %+v %v", jobs, err)
		}
		dup := jobs[0]
		if err := sub.Store.Insert(ctx, dup); err == nil {
			t.Fatal("a place used twice")
		}
	})
	t.Run("vouchers are numbered once, without gaps", func(t *testing.T) {
		sub := newSubject(t)
		a, b := sub.NewEntry(t), sub.NewEntry(t)
		if _, found, _ := sub.Store.Voucher(ctx, a); found {
			t.Fatal("a voucher before assigning")
		}
		n1, _ := sub.Store.AssignVoucher(ctx, a)
		n2, _ := sub.Store.AssignVoucher(ctx, b)
		again, _ := sub.Store.AssignVoucher(ctx, a)
		if n1 != 1 || n2 != 2 || again != 1 {
			t.Fatalf("numbers %d %d %d", n1, n2, again)
		}
		if n, found, _ := sub.Store.Voucher(ctx, b); !found || n != 2 {
			t.Fatal("voucher of b")
		}
	})
}
