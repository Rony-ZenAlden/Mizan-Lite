package catalog

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
)

// Re-pricing when the exchange rate moves, and the open-priced "Miscellaneous" item (the owner's request, 2026-09-23).

// Proposal is one product whose price the dollar has left behind, and the price proposed for it.
type Proposal struct {
	Product domain.Product
	// ShiftMicro is how far the rate has moved since the product was priced, at 10⁻⁶ of a percentage point, signed.
	ShiftMicro int64
	// ProposedMicro is the new price, in the product's own currency at 10⁻⁶, rounded to the shop's smallest note.
	ProposedMicro int64
}

// Proposals is a re-price the owner has not yet agreed to.
type Proposals struct {
	RateNano int64
	Local    string
	Items    []Proposal
}

// RepriceProposal lists every active product priced in the local currency whose price the rate has moved away from by
// StaleThresholdPercent or more, with a proposed price. Nothing is changed: this is what the owner reads before
// deciding.
//
// uniformPercent is empty to follow the rate — each product back to the dollar value it was priced at — or a signed
// percentage the owner typed to move every listed price by the same amount instead.
func (s *Service) RepriceProposal(ctx context.Context, uniformPercent string) (Proposals, error) {
	if s.rates == nil {
		return Proposals{}, nil
	}
	nano, local, found, err := s.rates.InForce(ctx)
	if err != nil || !found {
		return Proposals{}, err
	}
	out := Proposals{RateNano: nano, Local: local, Items: []Proposal{}}
	var uniform int64
	if uniformPercent != "" {
		if uniform, err = domain.ParseRepricePercent(uniformPercent); err != nil {
			return Proposals{}, err
		}
	}
	note, err := s.rates.CashNote(ctx)
	if err != nil {
		return Proposals{}, err
	}
	ref, err := s.Reference(ctx)
	if err != nil {
		return Proposals{}, err
	}
	step := domain.StepMicro(ref.Currencies[local], local, note)
	products, err := s.All(ctx)
	if err != nil {
		return Proposals{}, err
	}
	for _, p := range products {
		if !p.Active || !p.Stale(local, nano) {
			continue
		}
		shift, _ := p.RateShift(local, nano)
		proposed, _ := p.FollowRate(local, nano, step)
		if uniformPercent != "" {
			proposed = p.ByPercent(uniform, step)
		}
		// A proposal that lands back on the price the product already has is not a change worth confirming.
		if proposed == p.PriceMicro {
			continue
		}
		out.Items = append(out.Items, Proposal{Product: p, ShiftMicro: shift, ProposedMicro: proposed})
	}
	// Largest move first: those are the prices furthest from what they should be.
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := abs(out.Items[i].ShiftMicro), abs(out.Items[j].ShiftMicro)
		if a != b {
			return a > b
		}
		return out.Items[i].Product.NameAR < out.Items[j].Product.NameAR
	})
	return out, nil
}

// RepriceChange is one price the owner confirmed: the product at the version they saw, and the price they agreed to.
type RepriceChange struct {
	ID         id.ID
	RowVersion int64
	Price      string
}

// BulkReprice applies the prices the owner confirmed, all of them or none.
//
// Each goes through SetPrice, so each is an owner's act recorded in the owner's history with its old and new price, as
// a price changed one at a time would be — one PIN covers them all, because owner mode outlasts the transaction. A
// product edited by someone else since the proposal was read is refused (row version), and with it the whole batch:
// a re-price half applied is a shop with two price lists.
func (s *Service) BulkReprice(ctx context.Context, changes []RepriceChange) ([]domain.Product, error) {
	if len(changes) == 0 {
		return nil, errs.Validation(domain.CodeRepriceInvalid, "nothing was chosen to re-price")
	}
	var out []domain.Product
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		for _, c := range changes {
			current, err := s.store.Get(ctx, c.ID)
			if err != nil {
				return err
			}
			updated, err := s.SetPrice(ctx, SetPriceInput{ID: c.ID, RowVersion: c.RowVersion,
				Currency: current.PriceCurrency, Price: c.Price})
			if err != nil {
				return err
			}
			out = append(out, updated)
		}
		return nil
	})
	return out, err
}

// Names of the built-in open-priced item. Arabic first; the English beside it for a shop reading English.
const (
	OpenItemNameAR = "متفرقات"
	OpenItemNameEN = "Miscellaneous"
)

// CodeNoOpenItem is returned when the till asks for the Miscellaneous item and the shop cannot have one: a product of
// that name exists already and is not open-priced, or the open item was deactivated by the owner.
const CodeNoOpenItem = "lite.catalog.no_open_item"

// OpenItem is the product the till's "Misc" button sells: the first active open-priced product, or one created the
// first time it is asked for.
//
// Created here rather than seeded by a migration, so a shop that never presses the button never sees it, and so its
// name key is the one the catalogue computes rather than a copy of it typed into SQL.
//
// A deactivated open item is the owner's decision and is left alone: the till reports it rather than bringing it back.
func (s *Service) OpenItem(ctx context.Context, currency string) (domain.Product, error) {
	products, err := s.All(ctx)
	if err != nil {
		return domain.Product{}, err
	}
	deactivated := false
	for _, p := range products {
		if !p.OpenPrice {
			continue
		}
		if p.Active {
			return p, nil
		}
		deactivated = true
	}
	if deactivated {
		return domain.Product{}, errs.Conflict(CodeNoOpenItem, "the open-priced item was deactivated")
	}
	created, err := s.Create(ctx, domain.Draft{NameAR: OpenItemNameAR, NameEN: OpenItemNameEN, UnitCode: "piece",
		PriceCurrency: currency, OpenPrice: true})
	if errs.CodeOf(err) == domain.CodeDuplicateName {
		return domain.Product{}, errs.Conflict(CodeNoOpenItem, "a product of that name exists and is not open-priced")
	}
	return created, err
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
