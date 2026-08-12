package sales

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// The audited actions and codes this step adds.
const (
	ActionDocumentDrafted   = "sales.document.drafted"
	ActionLineAdded         = "sales.document.line_added"
	ActionLineRemoved       = "sales.document.line_removed"
	ActionDocumentHeld      = "sales.document.held"
	ActionDocumentResumed   = "sales.document.resumed"
	ActionDocumentCancelled = "sales.document.cancelled"

	EntityDocument = "sales.document"

	CodeUnknownDocument = "sales.unknown_document"
	CodeUnknownLine     = "sales.unknown_line"
	CodeCatalogMissing  = "sales.catalog_missing"
)

// Catalog is what sales needs to know about what it is selling.
//
// A PORT, not an import of catalog (§10.3). Sales needs four facts to snapshot a line: what the
// product is called, what its variant's SKU is, which unit it is sold in, and how to convert the
// customer's quantity into stock units.
//
// The conversion is here rather than done by sales, because unit arithmetic belongs to the module
// that owns units — and doing it twice is how "2 rolls" and "200 metres" stop agreeing.
type Catalog interface {
	// LineFacts reports what a line must snapshot about a variant, and converts the entered
	// quantity into the product's stock unit.
	LineFacts(
		ctx context.Context, companyID, variantID, uomID id.ID, quantityMicro int64,
	) (LineFacts, error)
}

// LineFacts is what the catalog says about a variant at the moment of sale.
type LineFacts struct {
	ProductID   id.ID
	ProductName string
	VariantSKU  string
	// UomID is the unit the line is priced in — the product's sales unit when the caller named
	// none.
	UomID   id.ID
	UomCode string
	// QuantityStockMicro is the entered quantity converted into the product's stock unit.
	QuantityStockMicro int64
}

// NewDocumentInput opens a draft.
type NewDocumentInput struct {
	CompanyID   id.ID
	BranchID    id.ID
	WarehouseID id.ID
	Type        domain.Type
	Date        string
	Currency    string

	PartnerID id.ID
	// PartnerName is snapshotted. A walk-in customer has neither, which is most of a shop's
	// trade and must not require a record.
	PartnerName string
	DueDate     string
	SourceID    id.ID
	Notes       string
}

// Draft opens a sales document.
func (s *Service) Draft(ctx context.Context, in NewDocumentInput) (domain.Document, error) {
	var created domain.Document

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		identifier, err := id.New()
		if err != nil {
			return err
		}
		built, err := domain.NewDocument(
			identifier, in.BranchID, in.Type, in.Date, in.Currency)
		if err != nil {
			return err
		}
		built.WarehouseID = in.WarehouseID
		built.PartnerID = in.PartnerID
		built.PartnerName = in.PartnerName
		built.DueDate = in.DueDate
		built.SourceID = in.SourceID
		built.Notes = in.Notes

		if err = s.repos.InsertDocument(
			txCtx, in.CompanyID, built, s.actorOf(txCtx)); err != nil {
			return err
		}
		created = built

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionDocumentDrafted, EntityType: EntityDocument, EntityID: built.ID,
			After: map[string]any{
				"type": string(built.Type), "date": built.Date,
				"currency": built.CurrencyCode,
			},
		})
	})
	if err != nil {
		return domain.Document{}, err
	}
	return created, nil
}

// AddLineInput puts an item on a draft.
//
// # There is no price here, deliberately
//
// The caller says WHAT and HOW MANY. The price comes from resolution at posting (Phase 3.5), the
// tax from the engine (Phase 2), and the cost from the costing port (Phase 4). A till operator
// who can type a price is a discount nobody approved, and an input struct with a price field is
// an invitation to build that screen.
type AddLineInput struct {
	CompanyID     id.ID
	DocumentID    id.ID
	VariantID     id.ID
	UomID         id.ID
	QuantityMicro int64
	Notes         string
	LotID         id.ID
	SerialID      id.ID
}

// AddLine puts an item on a draft.
//
// The SNAPSHOT is taken here, at the moment the line is added: what the product is called, what
// its SKU is, what the unit is called, and what the quantity converts to in stock units. Taking
// it at posting instead would be almost as good — and "almost" is the gap through which a product
// renamed between drafting and posting changes what the customer sees on the invoice they were
// quoted.
func (s *Service) AddLine(ctx context.Context, in AddLineInput) (domain.Line, error) {
	if s.catalog == nil {
		return domain.Line{}, errs.Internal(CodeCatalogMissing,
			"the sales service was built without a catalog")
	}

	var added domain.Line

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireDocument(txCtx, in.DocumentID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}

		facts, err := s.catalog.LineFacts(
			txCtx, in.CompanyID, in.VariantID, in.UomID, in.QuantityMicro)
		if err != nil {
			return err
		}

		position, err := s.repos.NextLineNumber(txCtx, in.DocumentID)
		if err != nil {
			return err
		}
		identifier, err := id.New()
		if err != nil {
			return err
		}
		line, err := domain.NewLine(identifier, facts.ProductID, in.VariantID,
			facts.UomID, position, in.QuantityMicro)
		if err != nil {
			return err
		}

		// The snapshot.
		line.ProductName = facts.ProductName
		line.VariantSKU = facts.VariantSKU
		line.UomCode = facts.UomCode
		line.QuantityStockMicro = facts.QuantityStockMicro
		line.LotID = in.LotID
		line.SerialID = in.SerialID
		line.Notes = in.Notes

		if err = s.repos.InsertLine(txCtx, in.DocumentID, line); err != nil {
			return err
		}
		added = line

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionLineAdded, EntityType: EntityDocument, EntityID: in.DocumentID,
			After: map[string]any{
				"line": position, "product": facts.ProductName,
				"quantity_micro": in.QuantityMicro, "uom": facts.UomCode,
			},
		})
	})
	if err != nil {
		return domain.Line{}, err
	}
	return added, nil
}

// RemoveLine takes an item off a draft.
func (s *Service) RemoveLine(ctx context.Context, documentID, lineID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireDocument(txCtx, documentID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}
		if err = s.repos.DeleteLine(txCtx, lineID); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionLineRemoved, EntityType: EntityDocument, EntityID: documentID,
			Before: map[string]any{"line_id": string(lineID)},
		})
	})
}

// Hold parks a draft so the next customer can be served (§2.6).
//
// A held sale is the SAME document, parked: it occupies no number and moves no stock. Modelling
// it as a different kind of thing would mean resuming it had to convert one into the other, and
// a conversion is a place for a line to get lost.
func (s *Service) Hold(ctx context.Context, documentID id.ID, label string) error {
	return s.setHeld(ctx, documentID, true, label, ActionDocumentHeld)
}

// Resume takes a held sale back off the shelf.
func (s *Service) Resume(ctx context.Context, documentID id.ID) error {
	return s.setHeld(ctx, documentID, false, "", ActionDocumentResumed)
}

func (s *Service) setHeld(
	ctx context.Context, documentID id.ID, held bool, label, action string,
) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireDocument(txCtx, documentID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}

		document.IsHeld = held
		document.HoldLabel = label
		if err = s.repos.UpdateDocument(txCtx, document, s.actorOf(txCtx)); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: action, EntityType: EntityDocument, EntityID: documentID,
			After: map[string]any{"held": held, "label": label},
		})
	})
}

// Cancel abandons a draft, leaving no number and no movements.
func (s *Service) Cancel(ctx context.Context, documentID id.ID) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		document, err := s.requireDocument(txCtx, documentID)
		if err != nil {
			return err
		}
		if err = document.RequireDraft(); err != nil {
			return err
		}

		// Written directly rather than through UpdateDocument, whose WHERE clause is guarded on
		// `status = 'draft'` — the guard that makes cancelling a posted document impossible would
		// also make cancelling a draft impossible if the status were changed first.
		if err = s.repos.SetDocumentStatus(
			txCtx, documentID, domain.Cancelled, "", ""); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionDocumentCancelled, EntityType: EntityDocument, EntityID: documentID,
			Before: map[string]any{"status": string(document.Status)},
			After:  map[string]any{"status": string(domain.Cancelled)},
		})
	})
}

// Document reads one document with its lines.
func (s *Service) Document(
	ctx context.Context, documentID id.ID,
) (domain.Document, []domain.Line, error) {
	document, found, err := s.repos.DocumentByID(ctx, documentID)
	if err != nil {
		return domain.Document{}, nil, err
	}
	if !found {
		return domain.Document{}, nil, errs.NotFound(CodeUnknownDocument,
			"there is no sales document with that identity")
	}
	lines, err := s.repos.Lines(ctx, documentID)
	if err != nil {
		return domain.Document{}, nil, err
	}
	return document, lines, nil
}

// Documents lists a company's documents, newest first.
func (s *Service) Documents(
	ctx context.Context, companyID id.ID, documentType domain.Type, status domain.Status,
) ([]domain.Document, error) {
	return s.repos.Documents(ctx, companyID, documentType, status)
}

func (s *Service) requireDocument(
	ctx context.Context, documentID id.ID,
) (domain.Document, error) {
	document, found, err := s.repos.DocumentByID(ctx, documentID)
	if err != nil {
		return domain.Document{}, err
	}
	if !found {
		return domain.Document{}, errs.NotFound(CodeUnknownDocument,
			"there is no sales document with that identity").
			WithParam("id", string(documentID))
	}
	return document, nil
}

// actorOf reads who is acting, for a document's created_by.
func (s *Service) actorOf(ctx context.Context) id.ID {
	if s.actors == nil {
		return id.ID("")
	}
	actor, ok := s.actors.Actor(ctx)
	if !ok {
		return id.ID("")
	}
	return actor.UserID
}
