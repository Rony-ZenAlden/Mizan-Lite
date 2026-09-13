package domain_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

var ref = domain.Reference{
	Units: map[string]domain.Unit{
		"kg": {Code: "kg", Kind: "mass", InputDecimals: 3}, "jar": {Code: "jar", Kind: "count"},
	},
	Currencies: map[string]domain.Currency{"SYP": {Code: "SYP", Decimals: 0}, "USD": {Code: "USD", Decimals: 2}},
}

func newID(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func valid() domain.Draft {
	return domain.Draft{NameAR: "زيت زيتون", NameEN: "Olive oil", UnitCode: "kg", PriceCurrency: "USD", Price: "3.25"}
}

func codeAndField(t *testing.T, err error) (string, string) {
	t.Helper()
	typed, ok := errs.AsError(err)
	if !ok {
		t.Fatalf("not a typed error: %v", err)
	}
	field := ""
	if len(typed.Fields) > 0 {
		field = typed.Fields[0].Field
	}
	return typed.Code, field
}

func TestNewProduct(t *testing.T) {
	p, err := domain.NewProduct(newID(t), valid(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Active || p.QuickSlot != 0 || p.RowVersion != 1 {
		t.Fatalf("a new product must be active, unpinned, version 1: %+v", p)
	}
	if p.PriceMicro != 3_250_000 || p.PriceCurrency != "USD" {
		t.Fatalf("price = %d %s", p.PriceMicro, p.PriceCurrency)
	}
}

func TestNewProductRefusals(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*domain.Draft)
		code, field string
	}{
		{"empty Arabic name", func(d *domain.Draft) { d.NameAR = "  " }, domain.CodeNameRequired, domain.FieldNameAR},
		{"a name of marks only has no key", func(d *domain.Draft) { d.NameAR = "ـــَ" }, domain.CodeNameRequired, domain.FieldNameAR},
		{"Arabic name too long", func(d *domain.Draft) { d.NameAR = strings.Repeat("ز", domain.MaxNameRunes+1) }, domain.CodeNameTooLong, domain.FieldNameAR},
		{"English name too long", func(d *domain.Draft) { d.NameEN = strings.Repeat("x", domain.MaxNameRunes+1) }, domain.CodeNameTooLong, domain.FieldNameEN},
		{"unknown unit", func(d *domain.Draft) { d.UnitCode = "gallon" }, domain.CodeUnknownUnit, domain.FieldUnit},
		{"unknown currency", func(d *domain.Draft) { d.PriceCurrency = "EUR" }, domain.CodeUnknownCurrency, domain.FieldCurrency},
		{"three decimals in USD", func(d *domain.Draft) { d.Price = "3.255" }, domain.CodePriceDecimals, domain.FieldPrice},
		{"any decimal in SYP", func(d *domain.Draft) { d.PriceCurrency, d.Price = "SYP", "45000.5" }, domain.CodePriceDecimals, domain.FieldPrice},
		{"grouping separator", func(d *domain.Draft) { d.PriceCurrency, d.Price = "SYP", "45,000" }, numinput.CodeGrouping, domain.FieldPrice},
		{"not a number", func(d *domain.Draft) { d.Price = "cheap" }, numinput.CodeInvalid, domain.FieldPrice},
		{"negative", func(d *domain.Draft) { d.Price = "-1" }, numinput.CodeInvalid, domain.FieldPrice},
		{"barcode with a space", func(d *domain.Draft) { d.Barcode = "622 300" }, domain.CodeBarcodeInvalid, domain.FieldBarcode},
		{"barcode too long", func(d *domain.Draft) { d.Barcode = strings.Repeat("1", domain.MaxBarcodeLength+1) }, domain.CodeBarcodeInvalid, domain.FieldBarcode},
		{"barcode with Arabic letters", func(d *domain.Draft) { d.Barcode = "زيت" }, domain.CodeBarcodeInvalid, domain.FieldBarcode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := valid()
			tc.mutate(&d)
			_, err := domain.NewProduct(newID(t), d, ref)
			code, field := codeAndField(t, err)
			if code != tc.code || field != tc.field {
				t.Fatalf("got %s on %q, want %s on %q", code, field, tc.code, tc.field)
			}
		})
	}
}

func TestPricesInEitherCurrencyAndDigitSet(t *testing.T) {
	for _, tc := range []struct {
		currency, price string
		micro           int64
	}{
		{"SYP", "٤٥٠٠٠", 45_000_000_000},
		{"USD", "٣٫٢٥", 3_250_000},
		{"USD", "0", 0}, // a free sample is a price, not a missing one
		{"USD", ".5", 500_000},
	} {
		d := valid()
		d.PriceCurrency, d.Price = tc.currency, tc.price
		p, err := domain.NewProduct(newID(t), d, ref)
		if err != nil || p.PriceMicro != tc.micro {
			t.Errorf("%s %q → %d, %v; want %d", tc.currency, tc.price, p.PriceMicro, err, tc.micro)
		}
	}
}

func TestABarcodeScannedOnAnArabicLayoutIsTheLatinBarcode(t *testing.T) {
	d := valid()
	d.Barcode = " ٦٢٢٣٠٠٠١١٢٣٤٥ "
	p, err := domain.NewProduct(newID(t), d, ref)
	if err != nil || p.Barcode != "6223000112345" {
		t.Fatalf("barcode = %q, %v", p.Barcode, err)
	}
}

func TestNamesAreTrimmedAndKeysNormalised(t *testing.T) {
	d := valid()
	d.NameAR, d.NameEN = "  إسطنبولي  ", "  Istanbul "
	p, err := domain.NewProduct(newID(t), d, ref)
	if err != nil {
		t.Fatal(err)
	}
	if p.NameAR != "إسطنبولي" || p.NameEN != "Istanbul" {
		t.Fatalf("names = %q %q", p.NameAR, p.NameEN)
	}
	if p.NameKey() != "اسطنبولي" {
		t.Fatalf("NameKey = %q", p.NameKey())
	}
	d.Barcode = "ABC123"
	p, _ = domain.NewProduct(newID(t), d, ref)
	if p.SearchText() != "اسطنبولي istanbul abc123" {
		t.Fatalf("SearchText = %q", p.SearchText())
	}
}

func TestDeactivationLeavesTheTillButton(t *testing.T) {
	p, _ := domain.NewProduct(newID(t), valid(), ref)
	p, err := p.WithSlot(7)
	if err != nil {
		t.Fatal(err)
	}
	p = p.Deactivate()
	if p.Active || p.QuickSlot != 0 {
		t.Fatalf("a deactivated product kept its button: %+v", p)
	}
	if p.Activate().QuickSlot != 0 {
		t.Fatal("reactivation restored a button somebody else may now hold")
	}
}

func TestSlots(t *testing.T) {
	p, _ := domain.NewProduct(newID(t), valid(), ref)
	for _, bad := range []int{-1, domain.QuickSlots + 1} {
		if _, err := p.WithSlot(bad); errs.CodeOf(err) != domain.CodeSlotInvalid {
			t.Errorf("slot %d: %v", bad, err)
		}
	}
	for _, good := range []int{0, 1, domain.QuickSlots} {
		if _, err := p.WithSlot(good); err != nil {
			t.Errorf("slot %d refused: %v", good, err)
		}
	}
	if _, err := p.Deactivate().WithSlot(3); errs.CodeOf(err) != domain.CodeSlotNeedsActive {
		t.Fatalf("an inactive product took a button: %v", err)
	}
}

func TestSamePrice(t *testing.T) {
	p, _ := domain.NewProduct(newID(t), valid(), ref)
	if !p.SamePrice("USD", 3_250_000) || p.SamePrice("USD", 3_250_001) || p.SamePrice("SYP", 3_250_000) {
		t.Fatal("SamePrice compares both the amount and the currency")
	}
}

func TestFormatMicroNeverRoundsForDisplay(t *testing.T) {
	for _, tc := range []struct {
		micro    int64
		decimals int
		want     string
	}{
		{3_250_000, 2, "3.25"},
		{3_000_000, 2, "3.00"},
		{45_000_000_000, 0, "45000"},
		{3_255_000, 2, "3.255"}, // more precision than the currency: shown, not rounded away
		{0, 2, "0.00"},
		{1, 0, "0.000001"},
	} {
		if got := domain.FormatMicro(tc.micro, tc.decimals); got != tc.want {
			t.Errorf("FormatMicro(%d, %d) = %q, want %q", tc.micro, tc.decimals, got, tc.want)
		}
	}
}

func TestErrorsAreTyped(t *testing.T) {
	if errs.CodeOf(domain.ErrStale()) != "database.concurrent_modification" {
		t.Fatal("a stale edit must use the platform's concurrency code, which already has a translation")
	}
	if !errs.IsCategory(domain.ErrNotFound(), errs.CategoryNotFound) {
		t.Fatal("not found is a NotFound")
	}
}
