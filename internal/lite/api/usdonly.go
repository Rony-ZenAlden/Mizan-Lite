package api

import (
	"context"
	"strings"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
	"github.com/mizan-erp/mizan/internal/lite/usdmode/domain"
)

// Dollars only (0.10.0, the owner's request of 2026-09-24): a shop that has gone over counts in dollars everywhere, and
// Go holds that rule at every door money comes in by — whatever a screen offers.

// CodeUSDOnlyByConversion refuses setting "usd" as a plain setting: going over converts the shop's prices and balances,
// which only the switch does.
const CodeUSDOnlyByConversion = "lite.settings.usd_only_by_conversion"

// localCurrencyOff refuses an amount in the local currency in a dollars-only shop, naming the field it came in.
func localCurrencyOff(shop moneyfmt.Shop, field string, currencies ...string) error {
	if !shop.USDOnly() {
		return nil
	}
	for _, c := range currencies {
		if c != "" && !strings.EqualFold(c, domain.USD) {
			return errs.Validation(domain.CodeLocalCurrencyOff, "the shop counts in dollars only").
				WithField(field, domain.CodeLocalCurrencyOff, "local currency").WithParam("currency", c)
		}
	}
	return nil
}

// orUSD is a currency a form left empty, in a dollars-only shop: dollars, not the local currency the books default to.
func orUSD(shop moneyfmt.Shop, currency string) string {
	if currency == "" && shop.USDOnly() {
		return domain.USD
	}
	return currency
}

// sellingCurrency is the currency a shop's own new prices are in by default: dollars in a dollars-only shop, the local
// currency otherwise.
func sellingCurrency(shop moneyfmt.Shop) string {
	if shop.USDOnly() {
		return domain.USD
	}
	return shop.Local
}

// USDPriceDTO is one product's price going over to dollars.
type USDPriceDTO struct {
	ProductID    string `json:"productId"`
	NameAR       string `json:"nameAr"`
	NameEN       string `json:"nameEn"`
	LocalPrice   string `json:"localPrice"`
	USDPrice     string `json:"usdPrice"`
	LocalCost    string `json:"localCost"`
	USDCost      string `json:"usdCost"`
	RaisedToCent bool   `json:"raisedToCent"`
}

// USDBalanceDTO is one customer's or supplier's balance going over to dollars.
type USDBalanceDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Local   string `json:"local"`
	Dollars string `json:"dollars"`
}

// USDOnlyPlanDTO is everything going over to dollars only will change, worked out by Go and not yet written. Token
// names it: the switch writes this plan and no other.
type USDOnlyPlanDTO struct {
	LocalCurrency string          `json:"localCurrency"`
	Rate          string          `json:"rate"`
	Prices        []USDPriceDTO   `json:"prices"`
	OpenItems     int             `json:"openItems"`
	Customers     []USDBalanceDTO `json:"customers"`
	Suppliers     []USDBalanceDTO `json:"suppliers"`
	DrawerLocal   string          `json:"drawerLocal"`
	DrawerDollars string          `json:"drawerDollars"`
	Token         string          `json:"token"`
}

func (v tillView) usdPlan(p domain.Plan) USDOnlyPlanDTO {
	local, usd := p.Money.Local, domain.USD
	dto := USDOnlyPlanDTO{LocalCurrency: local, Rate: rateText(v.shop, p.Money.RateNano), OpenItems: len(p.OpenItems),
		Prices: make([]USDPriceDTO, 0, len(p.Prices)), Customers: make([]USDBalanceDTO, 0, len(p.Customers)),
		Suppliers: make([]USDBalanceDTO, 0, len(p.Suppliers)), Token: p.Token}
	for _, c := range p.Prices {
		price := USDPriceDTO{ProductID: c.Product.ID.String(), NameAR: c.Product.NameAR, NameEN: c.Product.NameEN,
			LocalPrice: v.shop.Display(catalogdomain.FormatMicro(c.Product.LocalPriceMicro, p.Money.LocalDecimals), local),
			USDPrice:   catalogdomain.FormatMicro(c.USDPriceMicro, p.Money.USDDecimals), RaisedToCent: c.RaisedToCent}
		if c.Product.HasCost {
			price.LocalCost = v.shop.Display(catalogdomain.FormatMicro(c.Product.LocalCostMicro, p.Money.LocalDecimals), local)
			price.USDCost = catalogdomain.FormatMicro(c.USDCostMicro, p.Money.USDDecimals)
		}
		dto.Prices = append(dto.Prices, price)
	}
	for _, c := range p.Customers {
		dto.Customers = append(dto.Customers, USDBalanceDTO{ID: c.ID.String(), Name: c.Name, Local: v.money(c.LocalMinor, local), Dollars: v.money(c.USDMinor, usd)})
	}
	for _, c := range p.Suppliers {
		dto.Suppliers = append(dto.Suppliers, USDBalanceDTO{ID: c.ID.String(), Name: c.Name, Local: v.money(c.LocalMinor, local), Dollars: v.money(c.USDMinor, usd)})
	}
	dto.DrawerLocal, dto.DrawerDollars = v.money(p.Drawer.LocalMinor, local), v.money(p.Drawer.USDMinor, usd)
	return dto
}

// USDOnlyPlan is what going over to dollars only would change, at the rate in force. Owner only.
func (s *Settings) USDOnlyPlan() envelope.Result[USDOnlyPlanDTO] {
	return call(s.core, "Settings.USDOnlyPlan", func(ctx context.Context, app *bootstrap.App) (USDOnlyPlanDTO, error) {
		p, err := app.USDMode.Plan(ctx)
		if err != nil {
			return USDOnlyPlanDTO{}, err
		}
		v, err := newTillView(ctx, app)
		return v.usdPlan(p), err
	})
}

// SwitchToUSDOnly goes over to dollars only, writing the plan the token names — every price, balance and the drawer's
// cash, then the setting — or nothing: a figure that moved since returns lite.usdmode.plan_changed. The owner's act.
func (s *Settings) SwitchToUSDOnly(token string) envelope.Result[SettingsDTO] {
	return call(s.core, "Settings.SwitchToUSDOnly", func(ctx context.Context, app *bootstrap.App) (SettingsDTO, error) {
		if _, err := app.USDMode.Apply(ctx, token); err != nil {
			return SettingsDTO{}, err
		}
		current, err := app.Settings.Get(ctx)
		if err != nil {
			return SettingsDTO{}, err
		}
		return toSettingsDTO(current), nil
	})
}
