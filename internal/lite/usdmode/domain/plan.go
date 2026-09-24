// Package domain is the arithmetic of a shop going over to dollars only (0.10.0): what every price, balance and the
// drawer come to in dollars at one rate, and the token that ties what the owner was shown to what is written.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys.
const (
	// CodeNoRate refuses the conversion with no rate to convert at.
	CodeNoRate = "lite.usdmode.no_rate"
	// CodePlanChanged refuses a conversion whose figures moved since the owner was shown them.
	CodePlanChanged = "lite.usdmode.plan_changed"
	// CodeAlreadyUSDOnly refuses a second conversion.
	CodeAlreadyUSDOnly = "lite.usdmode.already_usd_only"
	// CodeLocalCurrencyOff refuses money in the local currency in a shop that counts in dollars only.
	CodeLocalCurrencyOff = "lite.usdmode.local_currency_off"
)

// USD is the code of the currency a dollars-only shop counts in.
const USD = "USD"

// Money is the two currencies and the rate a conversion is worked out at.
type Money struct {
	Local         string
	LocalDecimals int
	USDDecimals   int
	RateNano      int64
}

// Product is a product priced in the local currency: a price and a cost at 10⁻⁶ of the local major unit, or an
// open-priced item, which has neither.
type Product struct {
	ID              id.ID
	RowVersion      int64
	NameAR          string
	NameEN          string
	OpenPrice       bool
	LocalPriceMicro int64
	HasCost         bool
	LocalCostMicro  int64
}

// PriceChange is one product's price, and its cost, in dollars.
type PriceChange struct {
	Product       Product
	USDPriceMicro int64
	USDCostMicro  int64
	// RaisedToCent is a price, or a cost, below half a cent at the rate, which a dollar price cannot be: it becomes a
	// cent, and the owner is shown so before confirming.
	RaisedToCent bool
}

// Balance is one customer's or supplier's balance in the local currency: above nought owed one way, below the other.
type Balance struct {
	ID         id.ID
	Name       string
	LocalMinor int64
}

// BalanceChange is a balance and what it comes to in dollars.
type BalanceChange struct {
	ID         id.ID
	Name       string
	LocalMinor int64
	USDMinor   int64
}

// DrawerChange is the drawer's local cash and what it comes to in dollars; zero when there is none to convert.
type DrawerChange struct {
	LocalMinor int64
	USDMinor   int64
}

// Plan is everything going over to dollars only will change, worked out and not yet written.
type Plan struct {
	Money     Money
	Prices    []PriceChange
	OpenItems []Product
	Customers []BalanceChange
	Suppliers []BalanceChange
	Drawer    DrawerChange
	// Token names this plan exactly: the conversion writes only the plan the owner confirmed.
	Token string
}

// NewPlan works the conversion out. A drawer holding no local cash, or less than none — which a count should settle
// first — converts nothing.
func NewPlan(m Money, products []Product, customers, suppliers []Balance, drawerLocalMinor int64) (Plan, error) {
	if m.RateNano <= 0 {
		return Plan{}, errs.Conflict(CodeNoRate, "set an exchange rate before going over to dollars")
	}
	p := Plan{Money: m, Prices: []PriceChange{}, OpenItems: []Product{}, Customers: []BalanceChange{}, Suppliers: []BalanceChange{}}
	for _, product := range products {
		if product.OpenPrice {
			p.OpenItems = append(p.OpenItems, product)
			continue
		}
		c := PriceChange{Product: product}
		c.USDPriceMicro, c.RaisedToCent = m.unitToUSD(product.LocalPriceMicro)
		if product.HasCost {
			var raised bool
			c.USDCostMicro, raised = m.unitToUSD(product.LocalCostMicro)
			c.RaisedToCent = c.RaisedToCent || raised
		}
		p.Prices = append(p.Prices, c)
	}
	for _, b := range customers {
		p.Customers = append(p.Customers, BalanceChange{ID: b.ID, Name: b.Name, LocalMinor: b.LocalMinor, USDMinor: m.MinorToUSD(b.LocalMinor)})
	}
	for _, b := range suppliers {
		p.Suppliers = append(p.Suppliers, BalanceChange{ID: b.ID, Name: b.Name, LocalMinor: b.LocalMinor, USDMinor: m.MinorToUSD(b.LocalMinor)})
	}
	if drawerLocalMinor > 0 {
		p.Drawer = DrawerChange{LocalMinor: drawerLocalMinor, USDMinor: m.MinorToUSD(drawerLocalMinor)}
	}
	sort.Slice(p.Prices, func(i, j int) bool { return p.Prices[i].Product.NameAR < p.Prices[j].Product.NameAR })
	sort.Slice(p.Customers, func(i, j int) bool { return p.Customers[i].Name < p.Customers[j].Name })
	sort.Slice(p.Suppliers, func(i, j int) bool { return p.Suppliers[i].Name < p.Suppliers[j].Name })
	p.Token = p.token()
	return p, nil
}

// MinorToUSD is a local amount in dollars' minor units at the rate, half away from zero.
func (m Money) MinorToUSD(localMinor int64) int64 {
	// usd = local ÷ 10^ld ÷ (nano ÷ 10⁹) × 10^ud
	num := new(big.Int).Mul(big.NewInt(localMinor), pow10(9+m.USDDecimals))
	den := new(big.Int).Mul(big.NewInt(m.RateNano), pow10(m.LocalDecimals))
	return roundDiv(num, den)
}

// unitToUSD is a local unit price at 10⁻⁶ in dollars at 10⁻⁶, held to the dollar's decimals, half up — and a cent when
// it would round to nothing, since a dollar price is a price of at least a cent.
func (m Money) unitToUSD(localMicro int64) (int64, bool) {
	// usd micro = local micro × 10⁹ ÷ nano, then to the cent: a step of 10^(6−ud) micro.
	step := pow10(6 - m.USDDecimals)
	cents := roundDiv(new(big.Int).Mul(big.NewInt(localMicro), pow10(9)), new(big.Int).Mul(big.NewInt(m.RateNano), step))
	if cents <= 0 && localMicro > 0 {
		return step.Int64(), true
	}
	return cents * step.Int64(), false
}

// NoteText is the note every conversion entry carries: what it was converted to, and at what.
func (m Money) NoteText() string {
	return fmt.Sprintf("%s @ %s", USD, rateText(m.RateNano))
}

func rateText(nano int64) string {
	whole, frac := nano/1_000_000_000, nano%1_000_000_000
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%09d", whole, frac), "0")
}

// token is a digest of every figure the plan will write, in a fixed order.
func (p Plan) token() string {
	var b strings.Builder
	fmt.Fprintf(&b, "rate=%d local=%s|", p.Money.RateNano, p.Money.Local)
	for _, c := range p.Prices {
		fmt.Fprintf(&b, "p:%s:%d:%d:%d:%t:%d|", c.Product.ID, c.Product.RowVersion, c.Product.LocalPriceMicro, c.USDPriceMicro, c.Product.HasCost, c.USDCostMicro)
	}
	for _, o := range p.OpenItems {
		fmt.Fprintf(&b, "o:%s:%d|", o.ID, o.RowVersion)
	}
	for _, c := range p.Customers {
		fmt.Fprintf(&b, "c:%s:%d:%d|", c.ID, c.LocalMinor, c.USDMinor)
	}
	for _, c := range p.Suppliers {
		fmt.Fprintf(&b, "s:%s:%d:%d|", c.ID, c.LocalMinor, c.USDMinor)
	}
	fmt.Fprintf(&b, "d:%d:%d", p.Drawer.LocalMinor, p.Drawer.USDMinor)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// roundDiv divides half away from zero. The figures converted here are a shop's prices and balances, far inside int64.
func roundDiv(num, den *big.Int) int64 {
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if new(big.Int).Abs(new(big.Int).Lsh(r, 1)).Cmp(new(big.Int).Abs(den)) >= 0 {
		if num.Sign()*den.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q.Int64()
}
