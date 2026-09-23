package printing_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/printing"
	"github.com/mizan-erp/mizan/internal/lite/printing/printingtest"
)

func TestAReprintIsACopyAndAFailureIsNot(t *testing.T) {
	ctx := context.Background()
	svc := printing.NewService(litetest.Immediate{}, printingtest.NewFake(), clock.NewFixed(time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)))
	sale, _ := id.New()
	if n, _ := svc.NextCopy(ctx, sale); n != 1 {
		t.Fatal("the first print is not copy 1")
	}
	if _, err := svc.Record(ctx, printing.Job{Kind: printing.KindSale, SubjectID: sale, CopyNo: 1, Printer: "X", Path: "raw", ErrorCode: "lite.printers.send_failed"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.NextCopy(ctx, sale); n != 1 {
		t.Fatal("a failed job counted as printed")
	}
	j, err := svc.Record(ctx, printing.Job{Kind: printing.KindSale, SubjectID: sale, CopyNo: 1, Printer: "X", Path: "raw", Sent: true, ErrorCode: "ignored"})
	if err != nil || j.Seq != 2 || j.ErrorCode != "" || j.PrintedAt.IsZero() {
		t.Fatalf("%+v %v", j, err)
	}
	if n, _ := svc.NextCopy(ctx, sale); n != 2 {
		t.Fatal("a reprint is copy 2")
	}
	if n, _ := svc.NextCopy(ctx, ""); n != 1 {
		t.Fatal("a test page")
	}
	if _, err := svc.Record(ctx, printing.Job{Kind: "fax"}); errs.CodeOf(err) != printing.CodeUnknownKind {
		t.Fatal(err)
	}
	e, _ := id.New()
	if n, _ := svc.AssignVoucher(ctx, e); n != 1 {
		t.Fatal("voucher")
	}
	if n, found, _ := svc.Voucher(ctx, e); !found || n != 1 {
		t.Fatal("voucher read")
	}
	if jobs, _ := svc.Jobs(ctx, 0); len(jobs) != 2 {
		t.Fatal("jobs")
	}
}
