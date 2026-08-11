package catalog

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/catalog/infra/sqlite"
)

// The audited actions this step adds.
const (
	ActionVariantsGenerated = "catalog.variants.generated"
	ActionExclusionAdded    = "catalog.exclusion.added"
	ActionPackagingCreated  = "catalog.packaging.created"
	ActionBarcodeCreated    = "catalog.barcode.created"

	EntityVariants  = "catalog.variants"
	EntityPackaging = "catalog.packaging"
	EntityBarcode   = "catalog.barcode"
)

// Stable codes for this step's failures.
const (
	CodeUnknownBarcode   = "catalog.unknown_barcode"
	CodeUnknownValue     = "catalog.unknown_attribute_value"
	CodeUnknownPackaging = "catalog.unknown_packaging"
)

// PlanVariants works out what a product's variants should be, WITHOUT changing anything.
//
// The preview half of §2.4. A caller shows the counts — "20 new, 4 to deactivate, 0 to delete" —
// and only then calls ApplyVariantPlan. Adding a fifth colour to a product with four sizes
// silently creating twenty SKUs is exactly the surprise a careful ERP avoids.
func (s *Service) PlanVariants(
	ctx context.Context, companyID id.ID, productCode string,
) (domain.Plan, error) {
	product, err := s.ProductByCode(ctx, companyID, productCode)
	if err != nil {
		return domain.Plan{}, err
	}
	return s.planFor(ctx, product)
}

func (s *Service) planFor(ctx context.Context, product domain.Product) (domain.Plan, error) {
	links, err := s.repos.ProductAttributes(ctx, product.ID)
	if err != nil {
		return domain.Plan{}, err
	}
	dimensions := make([]domain.Dimension, 0, len(links))
	for _, link := range links {
		if !link.VariantDefining {
			continue
		}
		dimensions = append(dimensions, domain.Dimension{
			AttributeID:   link.Attribute.ID,
			AttributeCode: link.Attribute.Code,
			Values:        link.Values,
		})
	}

	exclusions, err := s.repos.Exclusions(ctx, product.ID)
	if err != nil {
		return domain.Plan{}, err
	}
	existing, err := s.repos.Variants(ctx, product.ID)
	if err != nil {
		return domain.Plan{}, err
	}
	return domain.GeneratePlan(product, dimensions, exclusions, existing)
}

// ApplyVariantPlan brings a product's variants into line with its attributes.
//
// Re-planned INSIDE the transaction rather than taking a plan the caller previewed. A plan is a
// snapshot of the catalog at the moment it was made, and between the preview and the click
// somebody may have added a colour or sold one of the variants the plan intended to delete —
// applying the stale plan would then delete a variant that now has history. Re-planning under
// the write lock is the only version of this that is safe, and the preview stays honest because
// it is the same function.
func (s *Service) ApplyVariantPlan(
	ctx context.Context, companyID id.ID, productCode string,
) (domain.Plan, error) {
	var applied domain.Plan

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		product, found, err := s.repos.ProductByCode(txCtx, companyID, upper(productCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownProduct,
				"there is no product with that code").WithParam("code", upper(productCode))
		}

		plan, err := s.planFor(txCtx, product)
		if err != nil {
			return err
		}
		if plan.IsEmpty() {
			// Nothing to do, and nothing to audit. An entry per click saying "no variants
			// changed" is the noise that trains people to ignore the trail.
			applied = plan
			return nil
		}

		for _, spec := range plan.Create {
			variantID, idErr := id.New()
			if idErr != nil {
				return idErr
			}
			if err = s.repos.InsertVariant(txCtx, domain.Variant{
				ID: variantID, ProductID: product.ID, SKU: spec.SKU,
				Combination: spec.Combination, IsActive: true,
			}); err != nil {
				return err
			}
			for _, assignment := range spec.Assignments {
				rowID, rowErr := id.New()
				if rowErr != nil {
					return rowErr
				}
				if err = s.repos.AssignValue(txCtx, rowID, variantID,
					assignment.AttributeID, assignment.ValueID); err != nil {
					return err
				}
			}
		}

		for _, variant := range plan.Reactivate {
			if err = s.repos.SetVariantActive(txCtx, variant.ID, true); err != nil {
				return err
			}
		}
		for _, variant := range plan.Deactivate {
			if err = s.repos.SetVariantActive(txCtx, variant.ID, false); err != nil {
				return err
			}
		}
		for _, variant := range plan.Delete {
			// The domain already decided this one has no history; asking again is the guard
			// that makes the decision impossible to bypass by calling the repository directly.
			if err = variant.RequireDeletable(); err != nil {
				return err
			}
			if err = s.repos.DeleteVariant(txCtx, variant.ID); err != nil {
				return err
			}
		}
		applied = plan

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionVariantsGenerated, EntityType: EntityVariants,
			EntityID: product.ID,
			After: map[string]any{
				"product": product.Code, "created": len(plan.Create),
				"reactivated": len(plan.Reactivate),
				"deactivated": len(plan.Deactivate), "deleted": len(plan.Delete),
			},
		})
	})
	if err != nil {
		return domain.Plan{}, err
	}
	return applied, nil
}

// ExcludeInput names a combination a product never makes.
type ExcludeInput struct {
	CompanyID   id.ID
	ProductCode string
	// Values are attribute-code/value-code pairs. Naming one pair excludes every combination
	// containing it; naming several excludes exactly the combinations containing all of them.
	Values map[string]string
	Reason string
}

// Exclude records a combination a product never makes.
func (s *Service) Exclude(ctx context.Context, in ExcludeInput) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		product, found, err := s.repos.ProductByCode(txCtx, in.CompanyID, upper(in.ProductCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownProduct,
				"there is no product with that code").WithParam("code", upper(in.ProductCode))
		}

		links, err := s.repos.ProductAttributes(txCtx, product.ID)
		if err != nil {
			return err
		}

		assignments := make([]domain.Assignment, 0, len(in.Values))
		for attributeCode, valueCode := range in.Values {
			assignment, resolveErr := resolveAssignment(links, attributeCode, valueCode)
			if resolveErr != nil {
				return resolveErr
			}
			assignments = append(assignments, assignment)
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		exclusion, err := domain.NewExclusion(identifier, assignments, in.Reason)
		if err != nil {
			return err
		}
		if err = s.repos.InsertExclusion(txCtx, product.ID, exclusion); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionExclusionAdded, EntityType: EntityVariants,
			EntityID: product.ID,
			After: map[string]any{
				"product": product.Code, "combination": exclusion.Combination,
				"reason": exclusion.Reason,
			},
		})
	})
}

// resolveAssignment turns an attribute code and a value code into the pair the domain works in.
//
// Resolved against what the PRODUCT offers, not against the whole dictionary: excluding a colour
// the product never offered would be a silent no-op, and a user who mistypes a value code
// deserves to hear about it rather than watch nothing happen.
func resolveAssignment(
	links []sqlite.ProductAttribute, attributeCode, valueCode string,
) (domain.Assignment, error) {
	for _, link := range links {
		if link.Attribute.Code != upper(attributeCode) {
			continue
		}
		for _, value := range link.Values {
			if value.Code == upper(valueCode) {
				return domain.Assignment{
					AttributeID:   link.Attribute.ID,
					AttributeCode: link.Attribute.Code,
					ValueID:       value.ID,
					ValueCode:     value.Code,
				}, nil
			}
		}
		return domain.Assignment{}, errs.NotFound(CodeUnknownValue,
			"this product does not offer that value").
			WithParam("attribute", upper(attributeCode)).WithParam("value", upper(valueCode))
	}
	return domain.Assignment{}, errs.NotFound(CodeUnknownValue,
		"this product does not use that attribute").WithParam("attribute", upper(attributeCode))
}

// ── packagings and barcodes ─────────────────────────────────────────────────────

// NewPackagingInput describes a product-specific bundle.
type NewPackagingInput struct {
	CompanyID     id.ID
	ProductCode   string
	Code          string
	Name          string
	NameKey       string
	QuantityMicro int64
}

// CreatePackaging adds a bundle to a product: a box of 12.
func (s *Service) CreatePackaging(
	ctx context.Context, in NewPackagingInput,
) (domain.Packaging, error) {
	var created domain.Packaging

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		product, found, err := s.repos.ProductByCode(txCtx, in.CompanyID, upper(in.ProductCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownProduct,
				"there is no product with that code").WithParam("code", upper(in.ProductCode))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		packaging, err := domain.NewPackaging(
			identifier, product.ID, in.Code, in.Name, in.QuantityMicro)
		if err != nil {
			return err
		}
		packaging.NameKey = in.NameKey

		if err = s.repos.InsertPackaging(txCtx, packaging); err != nil {
			return err
		}
		created = packaging

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPackagingCreated, EntityType: EntityPackaging,
			EntityID: packaging.ID,
			After: map[string]any{
				"product": product.Code, "code": packaging.Code,
				"quantity_micro": packaging.QuantityMicro,
			},
		})
	})
	if err != nil {
		return domain.Packaging{}, err
	}
	return created, nil
}

// Packagings lists a product's bundles.
func (s *Service) Packagings(
	ctx context.Context, productID id.ID,
) ([]domain.Packaging, error) {
	return s.repos.Packagings(ctx, productID)
}

// NewBarcodeInput attaches a barcode to a variant, optionally to a packaging of it.
type NewBarcodeInput struct {
	VariantID id.ID
	// PackagingCode, when set, makes this the barcode of a bundle: scanning it resolves to the
	// packaging's quantity rather than to one.
	PackagingCode string
	ProductID     id.ID
	Code          string
	Type          string
	IsPrimary     bool
}

// CreateBarcode attaches a barcode.
//
// The code is globally unique, kept by the schema. The check here exists to turn the constraint
// violation into a message naming the code and what already holds it, because "UNIQUE constraint
// failed: barcodes.code" tells an operator nothing about which product to go and look at.
func (s *Service) CreateBarcode(ctx context.Context, in NewBarcodeInput) (domain.Barcode, error) {
	var created domain.Barcode

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if existing, found, err := s.repos.BarcodeByCode(txCtx, in.Code); err != nil {
			return err
		} else if found {
			return errs.Conflict(domain.CodeBarcodeInUse,
				"this barcode is already in use").
				WithParam("code", in.Code).WithParam("variant", string(existing.VariantID))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		barcode, err := domain.NewBarcode(identifier, in.VariantID, in.Code, in.Type)
		if err != nil {
			return err
		}
		barcode.IsPrimary = in.IsPrimary

		if in.PackagingCode != "" {
			packaging, found, packErr := s.repos.PackagingByCode(
				txCtx, in.ProductID, upper(in.PackagingCode))
			if packErr != nil {
				return packErr
			}
			if !found {
				return errs.NotFound(CodeUnknownPackaging,
					"this product has no packaging with that code").
					WithParam("code", upper(in.PackagingCode))
			}
			barcode.PackagingID = packaging.ID
		}

		if err = s.repos.InsertBarcode(txCtx, barcode); err != nil {
			return err
		}
		created = barcode

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionBarcodeCreated, EntityType: EntityBarcode,
			EntityID: barcode.ID,
			After: map[string]any{
				"code": barcode.Code, "variant": string(barcode.VariantID),
				"packaging": string(barcode.PackagingID),
			},
		})
	})
	if err != nil {
		return domain.Barcode{}, err
	}
	return created, nil
}

// ScanBarcode resolves a scanned code to a variant and a quantity (§A.5).
//
// One variant and one quantity, always. Scanning the box adds 12; scanning the unit adds 1. A
// till calls this and needs no second lookup to know how many.
func (s *Service) ScanBarcode(ctx context.Context, code string) (domain.Scan, error) {
	barcode, found, err := s.repos.BarcodeByCode(ctx, code)
	if err != nil {
		return domain.Scan{}, err
	}
	if !found || !barcode.IsActive {
		// A retired barcode reads as unknown rather than as a stale variant: a code taken out
		// of service and then scanned should stop the sale, not quietly sell last year's item.
		return domain.Scan{}, errs.NotFound(CodeUnknownBarcode,
			"this barcode is not recognised").WithParam("code", code)
	}

	if barcode.PackagingID.IsZero() {
		return domain.Resolve(barcode, nil), nil
	}
	packaging, found, err := s.repos.PackagingByID(ctx, barcode.PackagingID)
	if err != nil {
		return domain.Scan{}, err
	}
	if !found {
		return domain.Scan{}, errs.NotFound(CodeUnknownPackaging,
			"this barcode names a packaging that no longer exists").WithParam("code", code)
	}
	return domain.Resolve(barcode, &packaging), nil
}

// Barcodes lists a variant's barcodes.
func (s *Service) Barcodes(ctx context.Context, variantID id.ID) ([]domain.Barcode, error) {
	return s.repos.Barcodes(ctx, variantID)
}
