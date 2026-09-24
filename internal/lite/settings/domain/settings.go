// Package domain holds Mizan Lite's settings as values: what exists, what it defaults to, and what
// counts as valid. No I/O.
package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
)

// CodeInvalidLocale is returned when a locale is not one Lite ships.
const CodeInvalidLocale = "lite.settings.invalid_locale"

// KeyLocale is the stored key for the interface language. It is the only place the string exists:
// call sites use Settings.Locale, never the key.
const KeyLocale = "ui.locale"

// KeyShopName is the stored key for the shop's name (L1 §8): the header now, the receipt from L7.
const KeyShopName = "shop.name"

// KeyRateMode is the stored key for how the exchange rate is kept (L3 §14.4). Written only through the fx module,
// because changing it needs the owner.
const KeyRateMode = "fx.mode"

// KeyRateAdjustPercent is the margin a shop puts on the internet's rate, at 10⁻⁶ of a percentage point, signed (L9.1, the
// owner's request of 2026-09-17). A shop that sells dollars above the published rate keeps the difference here instead of
// retyping a rate by hand every morning. Zero, the default, leaves the internet's figure exactly as it came.
const KeyRateAdjustPercent = "fx.adjust_percent"

// MaxRateAdjustPercentMicro bounds the margin at ±50 percentage points. A shop putting half again on the published rate is
// already extraordinary; past that a typing slip (5000 for 50) would price the whole shop wrongly.
const MaxRateAdjustPercentMicro = 50_000_000

// CodeInvalidRateAdjust is a margin that is not a number, or is past the bound.
const CodeInvalidRateAdjust = "lite.settings.invalid_rate_adjust"

// Where a fetched rate comes from (the owner's request, 2026-09-17). It is meaningful only in automatic mode: in manual
// mode nothing is fetched into force at all.
const (
	KeyRateSource = "fx.source"
	// RateSourceStandard is the published providers, with the shop's margin on top (0.9.3).
	RateSourceStandard = "standard"
	// RateSourceLocal is the shop's own local-market endpoint, falling back to the published providers when it fails.
	RateSourceLocal = "local"
)

// KeyLocalRateURL and KeyLocalRateField are the shop's own endpoint and the dotted path to the number inside its JSON.
// Empty means the shop has not set one up, and the local source behaves as the standard one.
const (
	KeyLocalRateURL   = "fx.local_url"
	KeyLocalRateField = "fx.local_field"
)

// CodeInvalidRateSource is a source that is neither standard nor local.
const CodeInvalidRateSource = "lite.settings.invalid_rate_source"

// ParseRateSource reads where fetched rates come from.
func ParseRateSource(raw string) (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "", RateSourceStandard:
		return RateSourceStandard, nil
	case RateSourceLocal:
		return RateSourceLocal, nil
	default:
		return "", errs.Validation(CodeInvalidRateSource, "a rate source is standard or local").WithParam("value", raw)
	}
}

// KeyLocalCurrency is the stored key for the currency the exchange rate prices (L3 §3.2). Not editable in v1: nothing
// writes it, and a stored value must name a currency code.
const KeyLocalCurrency = "currency.local"

// KeyMoneyDisplay is how the shop reads its own currency after the redenomination: the old pound, the new one, or both
// (L10, 2026-09-17). It changes what is shown and what a typed figure means — never what the books hold.
const KeyMoneyDisplay = "currency.display"

// CodeInvalidMoneyDisplay is a display that is not one of the three.
const CodeInvalidMoneyDisplay = "lite.settings.invalid_money_display"

// KeyPINRequired is the master switch for the owner's PIN (the owner's request, 2026-09-17).
//
// Off — the default, and what every shop has run since 2026-09-16 — a shop at a counter is never stopped for a PIN; the
// acts that matter are still written to the owner's history, which is what an owner reads afterwards. On, the PIN is
// asked for before every guarded act and before the figures an owner may not want a helper reading.
//
// Two acts ask regardless of the switch (owner.ReservedActs): restoring over the live database, and bringing in a
// foreign backup to restore from. Those destroy the books rather than change them, and a switch in the very settings
// they would overwrite is not a place to put the shop's last defence.
const KeyPINRequired = "security.pin_required"

// CodeInvalidPINRequired is a stored switch that is neither on nor off.
const CodeInvalidPINRequired = "lite.settings.invalid_pin_required"

// CodeInvalidRateMode is returned when a rate mode is not one Lite has.
const CodeInvalidRateMode = "lite.settings.invalid_rate_mode"

// RateMode is how the exchange rate is kept.
type RateMode string

// The rate modes. Manual is the default, by the owner's decision at L3's approval: the internet's rate is official-style
// and below the market rate the shop trades at (L3 §14.3 F2), so a fresh installation prices from the owner's rate and
// shows the internet's for reference. Automatic is one PIN away.
const (
	RateAutomatic RateMode = "automatic"
	RateManual    RateMode = "manual"
)

// ParseRateMode accepts exactly a rate mode.
func ParseRateMode(raw string) (RateMode, error) {
	switch RateMode(strings.TrimSpace(raw)) {
	case RateAutomatic:
		return RateAutomatic, nil
	case RateManual:
		return RateManual, nil
	default:
		return "", errs.Validation(CodeInvalidRateMode, "unknown rate mode").WithParam("value", raw)
	}
}

// KeyCashNote is the stored key for the smallest local-currency note the shop hands over: totals in the local
// currency round to it (L4 §3.2, Q-L4.1). Written only through the sales module, because changing it needs the owner.
const KeyCashNote = "currency.local_cash_note"

// Cash note bounds and codes.
const (
	// DefaultCashNote is 500 pounds (Q-L4.1).
	DefaultCashNote     = int64(500)
	MaxCashNote         = int64(1_000_000)
	CodeInvalidCashNote = "lite.settings.invalid_cash_note"
)

// ParseCashNote reads a note in whole local minor units: a positive integer up to MaxCashNote.
func ParseCashNote(raw string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 1 || v > MaxCashNote {
		return 0, errs.Validation(CodeInvalidCashNote, "the cash note must be a whole amount from 1 to 1,000,000").
			WithField("cashNote", CodeInvalidCashNote, "invalid").WithParam("max", strconv.FormatInt(MaxCashNote, 10))
	}
	return v, nil
}

// KeyDebtCurrency is the stored key for the currency a sale put on credit is charged in by default (L5 §5.1, Q-L5.1): a
// currency code — USD or the local currency — not "local"/"usd", so a redenomination changes no stored value (A-L5.4).
// The cashier switches it per sale; nothing writes it in L5.
const KeyDebtCurrency = "debt.default_currency"

// DefaultDebtCurrency is US dollars, by the owner's answer to Q-L5.1: a debt that keeps its value while it is owed.
const DefaultDebtCurrency = "USD"

// DefaultLocalCurrency is the local currency of a fresh installation (Q-L3.4).
const DefaultLocalCurrency = "SYP"

// Shop name codes and bound.
const (
	CodeShopNameRequired = "lite.settings.shop_name_required"
	CodeShopNameTooLong  = "lite.settings.shop_name_too_long"
	MaxShopNameRunes     = 100
	FieldShopName        = "shopName"
)

// ParseShopName trims a shop name and refuses an empty or over-long one.
func ParseShopName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errs.Validation(CodeShopNameRequired, "the shop name is required").
			WithField(FieldShopName, CodeShopNameRequired, "required")
	}
	if utf8.RuneCountInString(name) > MaxShopNameRunes {
		return "", errs.Validation(CodeShopNameTooLong, "the shop name is too long").
			WithField(FieldShopName, CodeShopNameTooLong, "too long").WithParam("max", strconv.Itoa(MaxShopNameRunes))
	}
	return name, nil
}

// Locale is a language Lite ships a complete catalog for.
type Locale string

// The shipped locales. Arabic is the primary language of the product (design D3).
const (
	Arabic  Locale = "ar"
	English Locale = "en"
)

// Direction is the writing direction a locale lays out in.
type Direction string

// Writing directions.
const (
	RTL Direction = "rtl"
	LTR Direction = "ltr"
)

// ParseLocale accepts exactly a shipped locale code, case- and space-insensitively.
//
// It does not accept a region ("ar-SY") or fall back to a nearby language. A settings row is
// written only by this application, so anything else in it is damage, and damage is reported
// rather than guessed at.
func ParseLocale(raw string) (Locale, error) {
	switch Locale(strings.ToLower(strings.TrimSpace(raw))) {
	case Arabic:
		return Arabic, nil
	case English:
		return English, nil
	default:
		return "", errs.Validation(CodeInvalidLocale, "unsupported locale").WithParam("value", raw)
	}
}

// Direction reports how the locale lays out.
func (l Locale) Direction() Direction {
	if l == Arabic {
		return RTL
	}
	return LTR
}

// Settings is every setting Lite has, resolved.
//
// A field is added here only when something reads it. The design lists a dozen settings for later
// phases; declaring them now would be the "built and connected to nothing" defect in its smallest
// form.
type Settings struct {
	Locale Locale
	// ShopName is empty until first run sets it.
	ShopName string
	// RateMode is how the exchange rate is kept (L3).
	RateMode RateMode
	// LocalCurrency is the code the exchange rate prices (L3).
	LocalCurrency string
	// CashNote is the smallest local note, in local minor units, that local totals round to (L4).
	CashNote int64
	// DebtCurrency is the currency a credit sale is charged in by default: USD or LocalCurrency (L5).
	DebtCurrency string
	// Receipt is the receipt printer and the receipt's header and footer (L7).
	Receipt Receipt
	// BackupFolder is where every backup is also copied — a USB drive or a synced folder — or empty (L7 §6.2).
	BackupFolder string
	// BackupEvery is how often the scheduler backs the shop up by itself: daily, weekly, monthly or manual only.
	BackupEvery string
	// RateAdjustPercentMicro is the margin put on a fetched rate, at 10⁻⁶ of a percentage point, signed. Zero leaves the
	// internet's figure as it came; it applies only in automatic mode, and never to a rate typed by hand.
	RateAdjustPercentMicro int64
	// MoneyDisplay is how the shop reads its local currency: "legacy", "new" or "dual" (L10) — or "usd", a shop gone over
	// to dollars only (0.10.0).
	MoneyDisplay string
	// PINRequired is whether guarded acts stop for the owner's PIN (2026-09-17). False on a fresh shop.
	PINRequired bool
	// RateSource is where a fetched rate comes from: the published providers, or the shop's own local-market endpoint
	// with the published ones behind it (2026-09-17).
	RateSource string
	// LocalRateURL is that endpoint, and LocalRateField the dotted path to the number in its JSON. Empty until a shop
	// sets one up, and while empty the local source behaves as the standard one.
	LocalRateURL   string
	LocalRateField string
}

// Defaults is what a fresh installation uses.
func Defaults() Settings {
	return Settings{Locale: Arabic, RateMode: RateManual, LocalCurrency: DefaultLocalCurrency, CashNote: DefaultCashNote,
		DebtCurrency: DefaultDebtCurrency, Receipt: DefaultReceipt(), BackupEvery: DefaultBackupEvery,
		RateSource: RateSourceStandard, MoneyDisplay: string(moneyfmt.Default)}
}

// ProblemKind says what was wrong with a stored row.
type ProblemKind string

// Problem kinds.
const (
	// UnknownKey is a row this build does not declare — written by a newer build, or left over
	// from an older one. Ignored, so an older binary still opens a newer database.
	UnknownKey ProblemKind = "unknown_key"
	// InvalidValue is a declared key whose value does not parse. The default is used instead.
	InvalidValue ProblemKind = "invalid_value"
)

// Problem is a stored row that was not used, and why.
type Problem struct {
	Key   string
	Value string
	Kind  ProblemKind
}

// FromStored resolves settings from stored rows.
//
// It never fails. Every problem is RETURNED for the caller to log, and the affected setting keeps
// its default — a damaged preference must not be able to stop a shop from opening (Mizan 0.10 D3:
// data leftovers are reported and ignored; only code defects are fatal).
func FromStored(rows map[string]string) (Settings, []Problem) {
	out := Defaults()
	var problems []Problem
	for key, value := range rows {
		switch key {
		case KeyLocale:
			locale, err := ParseLocale(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.Locale = locale
		case KeyShopName:
			name, err := ParseShopName(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.ShopName = name
		case KeyRateMode:
			mode, err := ParseRateMode(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.RateMode = mode
		case KeyLocalCurrency:
			if !isCurrencyCode(value) {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.LocalCurrency = value
		case KeyCashNote:
			note, err := ParseCashNote(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.CashNote = note
		case KeyDebtCurrency:
			if !isCurrencyCode(value) {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.DebtCurrency = value
		case KeyMoneyDisplay:
			if !moneyfmt.Valid(value) {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.MoneyDisplay = string(moneyfmt.Parse(value))
		case KeyPINRequired:
			switch value {
			case "true":
				out.PINRequired = true
			case "false":
				out.PINRequired = false
			default:
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
			}
		case KeyRateSource:
			source, err := ParseRateSource(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.RateSource = source
		case KeyLocalRateURL:
			out.LocalRateURL = value
		case KeyLocalRateField:
			out.LocalRateField = value
		case KeyRateAdjustPercent:
			adjust, err := ParseRateAdjustPercent(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.RateAdjustPercentMicro = adjust
		default:
			handled, err := out.storedPrinting(key, value)
			switch {
			case !handled:
				problems = append(problems, Problem{Key: key, Value: value, Kind: UnknownKey})
			case err != nil:
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				defaults := Defaults()
				out.Receipt = mergeDefault(out.Receipt, defaults.Receipt, key)
			}
		}
	}
	// Only now is the local currency known: a debt currency must be it or dollars (rows arrive in no order).
	if out.DebtCurrency != DefaultDebtCurrency && out.DebtCurrency != out.LocalCurrency {
		problems = append(problems, Problem{Key: KeyDebtCurrency, Value: out.DebtCurrency, Kind: InvalidValue})
		out.DebtCurrency = DefaultDebtCurrency
	}
	return out, problems
}

// Change is one stored row to write.
type Change struct {
	Key   string
	Value string
}

// Update is a partial change to settings. A nil field is left as it is.
type Update struct {
	Locale   *string
	ShopName *string
	RateMode *string
	// RateAdjustPercent is the margin on the internet's rate, as typed ("5", "-2٫5"); "" leaves it as it is.
	RateAdjustPercent *string
	// MoneyDisplay is how the shop reads its currency (L10).
	MoneyDisplay *string
	// PINRequired is the master switch for the owner's PIN (2026-09-17).
	PINRequired *bool
	// RateSource, LocalRateURL and LocalRateField are where fetched rates come from (2026-09-17).
	RateSource     *string
	LocalRateURL   *string
	LocalRateField *string
	CashNote       *string
	Printing       PrintingUpdate
}

// Apply validates an update against the current settings and returns the result and the rows that
// must be written. Nothing is returned to write for a field whose value does not change.
func (s Settings) Apply(u Update) (Settings, []Change, error) {
	next := s
	var changes []Change
	if u.Locale != nil {
		locale, err := ParseLocale(*u.Locale)
		if err != nil {
			return s, nil, err
		}
		if locale != s.Locale {
			next.Locale = locale
			changes = append(changes, Change{Key: KeyLocale, Value: string(locale)})
		}
	}
	if u.ShopName != nil {
		name, err := ParseShopName(*u.ShopName)
		if err != nil {
			return s, nil, err
		}
		if name != s.ShopName {
			next.ShopName = name
			changes = append(changes, Change{Key: KeyShopName, Value: name})
		}
	}
	if u.RateMode != nil {
		mode, err := ParseRateMode(*u.RateMode)
		if err != nil {
			return s, nil, err
		}
		if mode != s.RateMode {
			next.RateMode = mode
			changes = append(changes, Change{Key: KeyRateMode, Value: string(mode)})
		}
	}
	if u.MoneyDisplay != nil {
		if !moneyfmt.Valid(*u.MoneyDisplay) {
			return s, nil, errs.Validation(CodeInvalidMoneyDisplay, "a money display is legacy, new, dual or usd").
				WithField("moneyDisplay", CodeInvalidMoneyDisplay, "unknown").WithParam("value", *u.MoneyDisplay)
		}
		if chosen := string(moneyfmt.Parse(*u.MoneyDisplay)); chosen != s.MoneyDisplay {
			next.MoneyDisplay = chosen
			changes = append(changes, Change{Key: KeyMoneyDisplay, Value: chosen})
		}
	}
	if u.PINRequired != nil && *u.PINRequired != s.PINRequired {
		next.PINRequired = *u.PINRequired
		changes = append(changes, Change{Key: KeyPINRequired, Value: strconv.FormatBool(*u.PINRequired)})
	}
	if u.RateSource != nil {
		source, err := ParseRateSource(*u.RateSource)
		if err != nil {
			return s, nil, err
		}
		if source != s.RateSource {
			next.RateSource = source
			changes = append(changes, Change{Key: KeyRateSource, Value: source})
		}
	}
	if u.LocalRateURL != nil && *u.LocalRateURL != s.LocalRateURL {
		next.LocalRateURL = *u.LocalRateURL
		changes = append(changes, Change{Key: KeyLocalRateURL, Value: *u.LocalRateURL})
	}
	if u.LocalRateField != nil && *u.LocalRateField != s.LocalRateField {
		next.LocalRateField = *u.LocalRateField
		changes = append(changes, Change{Key: KeyLocalRateField, Value: *u.LocalRateField})
	}
	if u.RateAdjustPercent != nil {
		adjust, err := ParseRateAdjustPercent(*u.RateAdjustPercent)
		if err != nil {
			return s, nil, err
		}
		if adjust != s.RateAdjustPercentMicro {
			next.RateAdjustPercentMicro = adjust
			changes = append(changes, Change{Key: KeyRateAdjustPercent, Value: FormatRateAdjustPercent(adjust)})
		}
	}
	if u.CashNote != nil {
		note, err := ParseCashNote(*u.CashNote)
		if err != nil {
			return s, nil, err
		}
		if note != s.CashNote {
			next.CashNote = note
			changes = append(changes, Change{Key: KeyCashNote, Value: strconv.FormatInt(note, 10)})
		}
	}
	return next.applyPrinting(u.Printing, changes)
}

// isCurrencyCode reports three upper-case ASCII letters — a code's shape; whether it exists is the database's to say.
func isCurrencyCode(v string) bool {
	if len(v) != 3 {
		return false
	}
	for _, r := range v {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
