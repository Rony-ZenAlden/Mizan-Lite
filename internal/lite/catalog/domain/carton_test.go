package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// TestACartonIsCountedInTheProductsOwnUnit: 6 jars to a carton, 12.5 kg to a sack; the unit's decimals bound what may be
// typed, and nothing is not a carton.
func TestACartonIsCountedInTheProductsOwnUnit(t *testing.T) {
	jar := domain.Product{UnitCode: "jar"}
	if p, err := jar.WithCartonSize("٦", ref); err != nil || p.UnitsPerCartonMicro != 6_000_000 {
		t.Fatalf("6 jars in Arabic digits = %d, %v", p.UnitsPerCartonMicro, err)
	}
	kg := domain.Product{UnitCode: "kg"}
	if p, err := kg.WithCartonSize("12.5", ref); err != nil || p.UnitsPerCartonMicro != 12_500_000 {
		t.Fatalf("12.5 kg = %d, %v", p.UnitsPerCartonMicro, err)
	}
	for raw, code := range map[string]string{"1.5": domain.CodeCartonDecimals, "0": domain.CodeCartonNotPositive, "abc": numinput.CodeInvalid} {
		if _, err := jar.WithCartonSize(raw, ref); err == nil {
			t.Errorf("%q was taken", raw)
		} else if c, f := codeAndField(t, err); c != code || f != domain.FieldCarton {
			t.Errorf("%q: %s on %s, want %s on %s", raw, c, f, code, domain.FieldCarton)
		}
	}
	set, _ := jar.WithCartonSize("6", ref)
	if cleared, err := set.WithCartonSize("", ref); err != nil || cleared.UnitsPerCartonMicro != 0 {
		t.Fatal("an empty carton size does not clear it")
	}
}

func TestANewProductTakesItsCartonSize(t *testing.T) {
	d := valid()
	d.UnitsPerCarton = "24"
	p, err := domain.NewProduct(newID(t), d, ref)
	if err != nil || p.UnitsPerCartonMicro != 24_000_000 {
		t.Fatalf("NewProduct = %d, %v", p.UnitsPerCartonMicro, err)
	}
}
