// Package imports brings products and partners in from a spreadsheet.
//
// # The one rule this package exists to keep
//
// **An import calls the same service a screen calls, row by row, and has no SQL of its own.**
//
// The temptation is to write fast: read the file, `INSERT` the rows, done. Every product imported
// that way skips the code that refuses a duplicate code, resolves the unit, creates the default
// variant, and writes the audit entry — so a shop's first four thousand products would be the only
// ones in the database that no invariant was ever applied to.
//
// 5.4 recorded the general shape: *when a test needs a helper that imitates a production
// mechanism, that mechanism is untested.* An importer that imitates a service is worse — the
// mechanism exists and is being bypassed.
//
// It is slower. A shop importing four thousand products waits a few seconds longer, once.
package imports

import (
	"context"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/tabular"
)

// Stable codes.
const (
	CodeImportFailed = "imports.failed"
	CodeUnknownKind  = "imports.unknown_kind"
)

// The permissions. An import CREATES records, so it demands the permission to create them —
// there is no separate "may import", because that would be a second way to grant the same power.
//
// The VALUES are the ones the catalog and partner modules declare. The first version invented
// `partner.manage`, which no module declares — so the binding requiring it would have been
// permanently unreachable, and the 7.6 coverage check caught it before anything shipped.
const (
	PermImportProducts = "catalog.manage"
	PermImportPartners = "partner.customer.manage"
)

// Kind is what a file contains.
type Kind string

// The kinds this phase supports. Transactions are deliberately absent: importing a posted invoice
// means importing its stock movements, its journal entries and its numbering, and a mistake there
// is unwindable only by hand.
const (
	Products Kind = "products"
	Partners Kind = "partners"
)

// RowOutcome is what happened to one row.
type RowOutcome struct {
	// Line is the row's position in the FILE, counting the header as line 1 — so it matches what
	// the user sees in their spreadsheet. An index into the parsed rows would be off by one and
	// send them to the wrong line.
	Line int
	// Key is what identifies the row to a person: a product code, a partner code.
	Key string
	// OK is whether it went in — or, in a dry run, whether it would have.
	OK bool
	// Message explains a failure, already translated by the error's own code.
	Message string
	// Code is the failure's stable code, for a screen that wants to group failures.
	Code string
}

// Report is what an import did.
type Report struct {
	// DryRun says whether anything was written.
	DryRun bool
	Kind   Kind

	Total     int
	Succeeded int
	Failed    int

	// Rows carries every outcome, in file order.
	//
	// Every row, not only the failures: a user who imported 4,000 products and got 12 failures
	// wants to see which 12, and one who got none wants to see that the file was read at all.
	Rows []RowOutcome
}

// Catalog is the narrow surface importing products needs.
//
// The REAL service satisfies it. An interface here rather than the concrete type because this
// module must not import catalog's internals — and because the interface is the statement of what
// an import is allowed to do: create a product, and nothing else.
type Catalog interface {
	CreateProduct(ctx context.Context, in CatalogProduct) error
}

// PartnerBook is the narrow surface importing partners needs.
//
// Named for what it IS rather than for what it holds, because `Partners` is already the KIND a
// file can contain — and a type and a constant sharing a name is the kind of collision that
// compiles in some languages and confuses readers in all of them.
type PartnerBook interface {
	CreatePartner(ctx context.Context, in PartnerRecord) error
}

// CatalogProduct is one row's worth of product, as the catalog service will receive it.
type CatalogProduct struct {
	CompanyID id.ID
	Code      string
	Name      string
	Type      string
	StockUnit string
	Category  string
}

// PartnerRecord is one row's worth of partner.
type PartnerRecord struct {
	CompanyID        id.ID
	Code             string
	Name             string
	Type             string
	IsCustomer       bool
	IsSupplier       bool
	Phone            string
	Email            string
	TaxNumber        string
	PaymentTermsDays int
	CreditLimitMinor int64
}

// Service imports files.
type Service struct {
	catalog  Catalog
	partners PartnerBook
}

// New builds the service.
func New(catalog Catalog, partners PartnerBook) *Service {
	return &Service{catalog: catalog, partners: partners}
}

// Import parses a file and applies it, or reports what it would do.
//
// # Why a failing row does not discard the rows that succeeded
//
// A partial import that says exactly what did not go in is more useful than an all-or-nothing one
// that says only "row 3000 was bad". A shop with a 4,000-row file has better things to do than
// find the one bad row by bisection — and re-importing a file whose good rows are already in is
// safe, because the services refuse duplicates.
//
// A dry run is what the screen offers first. Importing four thousand products into a live shop is
// not a thing to do twice.
func (s *Service) Import(
	ctx context.Context, companyID id.ID, kind Kind, content []byte, dryRun bool,
) (Report, error) {
	switch kind {
	case Products:
		return s.importProducts(ctx, companyID, content, dryRun)
	case Partners:
		return s.importPartners(ctx, companyID, content, dryRun)
	default:
		return Report{}, errs.Validation(CodeUnknownKind,
			"that is not something this application can import").
			WithParam("kind", string(kind))
	}
}

func (s *Service) importProducts(
	ctx context.Context, companyID id.ID, content []byte, dryRun bool,
) (Report, error) {
	records, err := tabular.Read(content, "code", "name")
	if err != nil {
		return Report{}, err
	}

	report := Report{DryRun: dryRun, Kind: Products, Total: len(records)}
	for _, record := range records {
		outcome := RowOutcome{Line: record.Line, Key: record.Get("code"), OK: true}

		product := CatalogProduct{
			CompanyID: companyID,
			Code:      record.Get("code"),
			Name:      record.Get("name"),
			Type:      defaulted(record.Get("type"), "goods"),
			StockUnit: defaulted(record.Get("unit"), "PCS"),
			Category:  record.Get("category"),
		}

		if dryRun {
			// A dry run validates what it CAN without writing: everything the service would
			// check that does not need the database. It cannot catch a duplicate code, and
			// saying so on the screen is more honest than implying a clean dry run guarantees a
			// clean import.
			if product.Code == "" || product.Name == "" {
				outcome.OK = false
				outcome.Code = CodeImportFailed
				outcome.Message = "a product needs a code and a name"
			}
		} else if err = s.catalog.CreateProduct(ctx, product); err != nil {
			outcome.OK = false
			outcome.Code = errs.CodeOf(err)
			outcome.Message = err.Error()
		}

		if outcome.OK {
			report.Succeeded++
		} else {
			report.Failed++
		}
		report.Rows = append(report.Rows, outcome)
	}
	return report, nil
}

func (s *Service) importPartners(
	ctx context.Context, companyID id.ID, content []byte, dryRun bool,
) (Report, error) {
	records, err := tabular.Read(content, "code", "name")
	if err != nil {
		return Report{}, err
	}

	report := Report{DryRun: dryRun, Kind: Partners, Total: len(records)}
	for _, record := range records {
		outcome := RowOutcome{Line: record.Line, Key: record.Get("code"), OK: true}

		partner := PartnerRecord{
			CompanyID: companyID,
			Code:      record.Get("code"),
			Name:      record.Get("name"),
			Type:      defaulted(record.Get("type"), "company"),
			// A partner who is neither is a row nobody can use, so the default is CUSTOMER —
			// which is what most rows in most files are.
			IsCustomer: truthy(record.Get("customer")) || !record.Has("supplier"),
			IsSupplier: truthy(record.Get("supplier")),
			Phone:      record.Get("phone"),
			Email:      record.Get("email"),
			TaxNumber:  record.Get("tax_number"),
		}
		if days := record.Get("payment_terms_days"); days != "" {
			parsed, parseErr := strconv.Atoi(days)
			if parseErr != nil {
				outcome.OK = false
				outcome.Code = CodeImportFailed
				outcome.Message = "payment terms must be a whole number of days"
			}
			partner.PaymentTermsDays = parsed
		}
		if limit := record.Get("credit_limit_minor"); limit != "" && outcome.OK {
			parsed, parseErr := strconv.ParseInt(limit, 10, 64)
			if parseErr != nil {
				outcome.OK = false
				outcome.Code = CodeImportFailed
				// MINOR units, and the column says so. A file with "150.00" in it is a file
				// somebody will otherwise import as 150 fils.
				outcome.Message = "a credit limit must be a whole number of minor units"
			}
			partner.CreditLimitMinor = parsed
		}

		if outcome.OK {
			if dryRun {
				if partner.Code == "" || partner.Name == "" {
					outcome.OK = false
					outcome.Code = CodeImportFailed
					outcome.Message = "a partner needs a code and a name"
				}
			} else if err = s.partners.CreatePartner(ctx, partner); err != nil {
				outcome.OK = false
				outcome.Code = errs.CodeOf(err)
				outcome.Message = err.Error()
			}
		}

		if outcome.OK {
			report.Succeeded++
		} else {
			report.Failed++
		}
		report.Rows = append(report.Rows, outcome)
	}
	return report, nil
}

func defaulted(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// truthy reads the several things a person types for "yes".
//
// A spreadsheet column filled in by hand contains "yes", "y", "1", "true", "TRUE" and "نعم". An
// importer that accepted only one of them would reject most real files.
func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "y", "1", "true", "t", "نعم":
		return true
	}
	return false
}
