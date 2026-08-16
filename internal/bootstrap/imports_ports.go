package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/imports"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	partnerdomain "github.com/mizan-erp/mizan/internal/modules/partner/domain"
)

// importCatalog satisfies the importer with the REAL catalog service.
//
// # This adapter is the whole point of the import design
//
// It is four lines, and every one of them matters: `CreateProduct` refuses a duplicate code,
// resolves the unit, creates the default variant, and writes the audit entry — inside one
// transaction. An importer with its own INSERT would do none of that, and the shop's first four
// thousand products would be the only ones in the database no invariant was applied to.
type importCatalog struct{ catalog *catalog.Service }

var _ imports.Catalog = importCatalog{}

func (c importCatalog) CreateProduct(ctx context.Context, in imports.CatalogProduct) error {
	_, _, err := c.catalog.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID:    in.CompanyID,
		Code:         in.Code,
		Name:         in.Name,
		Type:         catalogdomain.ProductType(in.Type),
		StockUnit:    in.StockUnit,
		CategoryCode: in.Category,
	})
	return err
}

// importPartners satisfies the importer with the REAL partner service.
type importPartners struct{ partner *partner.Service }

var _ imports.PartnerBook = importPartners{}

func (p importPartners) CreatePartner(ctx context.Context, in imports.PartnerRecord) error {
	_, err := p.partner.CreatePartner(ctx, partner.NewPartnerInput{
		CompanyID:        in.CompanyID,
		Code:             in.Code,
		Name:             in.Name,
		Type:             partnerdomain.PartnerType(in.Type),
		IsCustomer:       in.IsCustomer,
		IsSupplier:       in.IsSupplier,
		Phone:            in.Phone,
		Email:            in.Email,
		TaxNumber:        in.TaxNumber,
		PaymentTermsDays: in.PaymentTermsDays,
		CreditLimitMinor: in.CreditLimitMinor,
	})
	return err
}
