package pricing

import (
	"context"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/pricing/domain"
)

// NewListInput describes a price list.
type NewListInput struct {
	CompanyID    id.ID
	Code         string
	Name         string
	NameKey      string
	CurrencyCode string
	Direction    domain.Direction
	IsDefault    bool
	ValidFrom    string
	ValidTo      string
}

// CreateList adds a price list.
func (s *Service) CreateList(ctx context.Context, in NewListInput) (domain.List, error) {
	var created domain.List

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.ListByCode(txCtx, in.CompanyID, upper(in.Code)); err != nil {
			return err
		} else if found {
			return errs.Conflict(CodeDuplicateCode,
				"a price list with this code already exists").WithParam("code", upper(in.Code))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		direction := in.Direction
		if direction == "" {
			direction = domain.Sale
		}
		list := domain.List{
			ID: identifier, Code: upper(in.Code), Name: strings.TrimSpace(in.Name),
			NameKey: in.NameKey, CurrencyCode: upper(in.CurrencyCode),
			IsDefault: in.IsDefault, Direction: direction,
			ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, IsActive: true,
		}
		if list.Name == "" || list.CurrencyCode == "" {
			return errs.Validation(domain.CodeInvalidList,
				"a price list needs a name and a currency")
		}

		if err = s.repos.InsertList(txCtx, in.CompanyID, list); err != nil {
			return err
		}
		created = list

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionListCreated, EntityType: EntityPriceList, EntityID: list.ID,
			After: map[string]any{
				"code": list.Code, "currency": list.CurrencyCode,
				"direction": string(list.Direction), "default": list.IsDefault,
			},
		})
	})
	if err != nil {
		return domain.List{}, err
	}
	return created, nil
}

// Lists returns a company's price lists for one direction.
func (s *Service) Lists(
	ctx context.Context, companyID id.ID, direction domain.Direction,
) ([]domain.List, error) {
	return s.repos.Lists(ctx, companyID, direction)
}

// ListItems returns every price in a list.
func (s *Service) ListItems(ctx context.Context, listID id.ID) ([]domain.Item, error) {
	return s.repos.ListItems(ctx, listID)
}

// SetPriceInput sets one price.
//
// Exactly one of ProductID and VariantID. A product-level price covers every variant; a
// variant-level one overrides it for a single variant.
type SetPriceInput struct {
	CompanyID   id.ID
	ListCode    string
	ProductID   id.ID
	VariantID   id.ID
	PriceMinor  int64
	MinQuantity int64
}

// SetPrice puts a price in a list.
func (s *Service) SetPrice(ctx context.Context, in SetPriceInput) (domain.Item, error) {
	var created domain.Item

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		list, err := s.requireList(txCtx, in.CompanyID, in.ListCode)
		if err != nil {
			return err
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		item, err := domain.NewItem(
			identifier, list.ID, in.ProductID, in.VariantID, in.PriceMinor, in.MinQuantity)
		if err != nil {
			return err
		}
		if err = s.repos.InsertItem(txCtx, item); err != nil {
			return err
		}
		created = item

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPriceSet, EntityType: EntityPriceList, EntityID: list.ID,
			After: map[string]any{
				"list": list.Code, "price_minor": item.PriceMinor,
				"min_quantity_micro": item.MinQuantity,
				"product":            string(item.ProductID), "variant": string(item.VariantID),
			},
		})
	})
	if err != nil {
		return domain.Item{}, err
	}
	return created, nil
}

// AssignToPartner gives a partner a price list.
//
// The direction must match the list's: assigning a purchase list as a customer's selling prices
// would silently sell at cost, and the mistake is invisible until someone reconciles a margin.
func (s *Service) AssignToPartner(
	ctx context.Context, companyID, partnerID id.ID, listCode string,
) error {
	return s.assign(ctx, companyID, listCode, func(txCtx context.Context, list domain.List) error {
		return s.repos.AssignToPartner(txCtx, partnerID, list.ID, list.Direction)
	}, "partner", partnerID)
}

// AssignToBranch gives a branch a price list.
func (s *Service) AssignToBranch(
	ctx context.Context, companyID, branchID id.ID, listCode string,
) error {
	return s.assign(ctx, companyID, listCode, func(txCtx context.Context, list domain.List) error {
		return s.repos.AssignToBranch(txCtx, branchID, list.ID, list.Direction)
	}, "branch", branchID)
}

func (s *Service) assign(
	ctx context.Context, companyID id.ID, listCode string,
	write func(context.Context, domain.List) error, kind string, target id.ID,
) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		list, err := s.requireList(txCtx, companyID, listCode)
		if err != nil {
			return err
		}
		if !list.IsActive {
			// Assigning a retired list would produce a partner whose prices silently fall
			// through to the default, which reads as a pricing bug rather than a stale setting.
			return errs.Validation(CodeUnknownList,
				"that price list is no longer in use").WithParam("code", list.Code)
		}
		if err = write(txCtx, list); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionListAssigned, EntityType: EntityPriceList, EntityID: list.ID,
			After: map[string]any{
				"list": list.Code, "direction": string(list.Direction),
				kind: string(target),
			},
		})
	})
}

// PriceQuery is what resolving a price needs.
type PriceQuery struct {
	CompanyID id.ID
	// PartnerID and BranchID are optional: a walk-in customer at the only shop has neither, and
	// resolution simply starts at the company default.
	PartnerID id.ID
	BranchID  id.ID
	ProductID id.ID
	VariantID id.ID
	// QuantityMicro decides which break applies. Pass the line's quantity, not one.
	QuantityMicro int64
	Direction     domain.Direction
	// Date the price applies on. Empty means today — a quote for a future date passes that date
	// so a seasonal list is honoured.
	Date string
}

// Price resolves what a variant costs, and records which list answered.
//
// # The chain is assembled here; the RULE lives in the domain
//
// This method's whole job is to load the three candidate lists in priority order and hand them
// to domain.Resolve. The order itself, the variant-before-product preference, the quantity
// break, and the refusal to invent zero are all in the pure function — testable against a table,
// and impossible to get subtly different in a second caller.
func (s *Service) Price(ctx context.Context, q PriceQuery) (domain.Resolved, error) {
	direction := q.Direction
	if direction == "" {
		direction = domain.Sale
	}
	date := q.Date
	if date == "" {
		date = clock.FormatDate(s.clk.Now())
	}

	chain := make([]domain.Candidate, 0, 3)

	if !q.PartnerID.IsZero() {
		list, found, err := s.repos.PartnerList(ctx, q.PartnerID, direction)
		if err != nil {
			return domain.Resolved{}, err
		}
		if found {
			candidate, buildErr := s.candidate(ctx, list,
				domain.SourcePartnerVariant, domain.SourcePartnerProduct, q)
			if buildErr != nil {
				return domain.Resolved{}, buildErr
			}
			chain = append(chain, candidate)
		}
	}

	if !q.BranchID.IsZero() {
		list, found, err := s.repos.BranchList(ctx, q.BranchID, direction)
		if err != nil {
			return domain.Resolved{}, err
		}
		if found {
			candidate, buildErr := s.candidate(ctx, list,
				domain.SourceBranchVariant, domain.SourceBranchProduct, q)
			if buildErr != nil {
				return domain.Resolved{}, buildErr
			}
			chain = append(chain, candidate)
		}
	}

	base, found, err := s.repos.DefaultList(ctx, q.CompanyID, direction)
	if err != nil {
		return domain.Resolved{}, err
	}
	if !found {
		// Resolution's last stop is missing. Distinguished from "this item has no price",
		// because the two need different answers: one is a setup step nobody completed, the
		// other is a product nobody priced.
		return domain.Resolved{}, errs.NotFound(domain.CodeNoDefaultList,
			"this company has no default price list").WithParam("direction", string(direction))
	}
	candidate, err := s.candidate(ctx, base,
		domain.SourceDefaultVariant, domain.SourceDefaultProduct, q)
	if err != nil {
		return domain.Resolved{}, err
	}
	chain = append(chain, candidate)

	return domain.Resolve(chain, q.VariantID, q.QuantityMicro, date)
}

func (s *Service) candidate(
	ctx context.Context, list domain.List, variantSource, productSource domain.Source,
	q PriceQuery,
) (domain.Candidate, error) {
	variantItems, productItems, err := s.repos.ItemsFor(
		ctx, list.ID, q.ProductID, q.VariantID)
	if err != nil {
		return domain.Candidate{}, err
	}
	return domain.Candidate{
		List: list, Source: variantSource, ProductSource: productSource,
		VariantItems: variantItems, ProductItems: productItems,
	}, nil
}
