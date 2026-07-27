package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

func TestConstructorsAndClassification(t *testing.T) {
	e := errs.Conflict("sales.invoice.not_draft", "invoice is not a draft")
	if !errs.IsCategory(e, errs.CategoryConflict) {
		t.Error("IsCategory(Conflict) should be true")
	}
	if errs.IsCategory(e, errs.CategoryValidation) {
		t.Error("IsCategory(Validation) should be false")
	}
	if errs.CategoryOf(e) != errs.CategoryConflict {
		t.Error("CategoryOf wrong")
	}
	if errs.CodeOf(e) != "sales.invoice.not_draft" {
		t.Error("CodeOf wrong")
	}
}

func TestWrapAndUnwrap(t *testing.T) {
	cause := errors.New("driver: unique violation")
	e := errs.Wrap(cause, errs.CategoryConflict, "database.duplicate", "duplicate")

	if !errors.Is(e, cause) {
		t.Error("errors.Is should find the wrapped cause")
	}
	// Classification still works through the wrapper.
	if !errs.IsCategory(e, errs.CategoryConflict) {
		t.Error("wrapped error should classify as Conflict")
	}
	// And through a further wrap by fmt.Errorf.
	outer := fmt.Errorf("saving product: %w", e)
	if !errs.IsCategory(outer, errs.CategoryConflict) {
		t.Error("classification should survive an outer fmt wrap")
	}
	if errs.CodeOf(outer) != "database.duplicate" {
		t.Error("CodeOf should survive an outer fmt wrap")
	}
}

func TestBuilders(t *testing.T) {
	e := errs.Validation("catalog.product.invalid", "invalid product").
		WithParam("field", "code").
		WithField("code", "required", "code is required")

	if e.Params["field"] != "code" {
		t.Error("WithParam")
	}
	if len(e.Fields) != 1 || e.Fields[0].Field != "code" {
		t.Error("WithField")
	}
}

func TestDefaultsForForeignErrors(t *testing.T) {
	plain := errors.New("some other error")
	if errs.CategoryOf(plain) != errs.CategoryInternal {
		t.Error("foreign error should default to Internal")
	}
	if errs.CodeOf(plain) != "" {
		t.Error("foreign error should have empty code")
	}
	if errs.IsCategory(plain, errs.CategoryConflict) {
		t.Error("foreign error is not a Conflict")
	}
}

func TestCategoryString(t *testing.T) {
	cases := map[errs.Category]string{
		errs.CategoryInternal:   "internal",
		errs.CategoryValidation: "validation",
		errs.CategoryNotFound:   "not_found",
		errs.CategoryConflict:   "conflict",
		errs.CategoryPermission: "permission",
	}
	for c, want := range cases {
		if c.String() != want {
			t.Errorf("%d.String() = %q, want %q", c, c.String(), want)
		}
	}
}
