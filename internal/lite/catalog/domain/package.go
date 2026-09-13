package domain

import (
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Package error codes. They double as i18n keys.
const (
	CodePackageItself          = "lite.catalog.package_itself"
	CodePackageNotCounted      = "lite.catalog.package_not_counted"
	CodePackageNested          = "lite.catalog.package_nested"
	CodePackageContentRequired = "lite.catalog.package_content_required"
	CodePackageContentDecimals = "lite.catalog.package_content_decimals"
)

// Package fields named in validation errors.
const (
	FieldPackageContent  = "contentProductId"
	FieldPackageQuantity = "contentQuantity"
)

// UnitKindCount is the kind of unit a package is sold in: tins, bottles, boxes — never kilos.
const UnitKindCount = "count"

// Package links a package product to what it opens into: one 16-litre tin → 16 litres of loose olive oil (L2 §3.5).
type Package struct {
	PackageProductID id.ID
	ContentProductID id.ID
	// ContentQuantityMicro is the content in one package, at 10⁻⁶ of the content product's unit.
	ContentQuantityMicro int64
	RowVersion           int64
}

// NewPackage validates a link between two products. That neither is part of another link — one level only
// (Q-L2.8) — is the service's to check: it depends on other rows.
func NewPackage(pkg, content Product, rawQuantity string, ref Reference) (Package, error) {
	if pkg.ID == content.ID {
		return Package{}, errs.Validation(CodePackageItself, "a product cannot open into itself").
			WithField(FieldPackageContent, CodePackageItself, "itself")
	}
	if ref.Units[pkg.UnitCode].Kind != UnitKindCount {
		return Package{}, errs.Validation(CodePackageNotCounted, "only a product sold by count opens").
			WithField(FieldPackageContent, CodePackageNotCounted, "not counted").WithParam("unit", pkg.UnitCode)
	}
	contentUnit := ref.Units[content.UnitCode]
	normalised, err := numinput.Normalise(rawQuantity)
	if err != nil {
		return Package{}, withField(err, FieldPackageQuantity)
	}
	if numinput.Decimals(normalised) > contentUnit.InputDecimals {
		return Package{}, errs.Validation(CodePackageContentDecimals, "too many decimals for the content's unit").
			WithField(FieldPackageQuantity, CodePackageContentDecimals, "too many decimals").
			WithParam("decimals", strconv.Itoa(contentUnit.InputDecimals))
	}
	amount, err := money.ParseUnitAmount(microParser, normalised)
	if err != nil || amount.Micro() <= 0 {
		return Package{}, errs.Validation(CodePackageContentRequired, "say how much one package holds").
			WithField(FieldPackageQuantity, CodePackageContentRequired, "required")
	}
	return Package{PackageProductID: pkg.ID, ContentProductID: content.ID, ContentQuantityMicro: amount.Micro()}, nil
}

// ContentText is the content quantity formatted in the content unit's decimals: "16.000" litres.
func (p Package) ContentText(content Product, ref Reference) string {
	return FormatMicro(p.ContentQuantityMicro, ref.Units[content.UnitCode].InputDecimals)
}

// microParser reads a plain decimal at 10⁻⁶; its currency is only the parser's carrier.
var microParser, _ = money.NewCurrency("XXX", 0, round.HalfUp)
