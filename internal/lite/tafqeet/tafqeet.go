// Package tafqeet writes an amount in words — التفقيط, the line at the foot of an invoice that says the total the way a
// person reads it aloud: "فقط ألف وثلاثمائة وأربعة وثلاثون دولاراً أمريكياً وخمسون سنتاً لا غير" (2026-09-24).
//
// # Why it is its own package, and why it is grammar and not a lookup
//
// An amount in words is a legal statement of the sum: it exists so that a figure altered with a pen no longer agrees
// with the words beside it. So it must be right, and Arabic counting is not a lookup table. The number decides the
// counted noun's form — دولار واحد، دولاران، ثلاثة دولارات، أحد عشر دولاراً، مائة دولار — and the noun's gender decides
// the number's: ثلاثة دولارات but ثلاث ليرات, أحد عشر دولاراً but إحدى عشرة ليرة. The thousands and millions are nouns
// counted in their turn (ثلاثة آلاف، أحد عشر ألفاً), and a scale word followed directly by the currency takes its
// construct form (ألفا دولار، أحد عشر ألف دولار). Each rule below is one of those, and the tests hold each one.
//
// # Numbers only
//
// The amount arrives as the plain decimal Go already formatted — Latin digits and at most one point — and is read as
// integers: never a float (DESIGN D9). Pure: standard library and kernel errors only (lite-pure-text).
package tafqeet

import (
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// CodeInvalid refuses an amount that cannot be written in words: not a plain non-negative decimal, more decimals than
// the currency's small unit has, or a trillion or more.
const CodeInvalid = "lite.tafqeet.invalid"

// Noun is a counted unit — a currency's large or small unit — in every form counting asks for.
type Noun struct {
	// Feminine is the noun's grammatical gender, which the numbers 1–2 agree with and 3–19 take the opposite of.
	Feminine bool
	// One is the singular, after 1 and after a round hundred or thousand: دولار أمريكي، ليرة سورية.
	One string
	// Two is the dual, which says "two" by itself: دولاران أمريكيان، ليرتان سوريتان.
	Two string
	// Plural follows 3–10: دولارات أمريكية، ليرات سورية.
	Plural string
	// Tamyeez is the singular in the accusative, after 11–99: دولاراً أمريكياً.
	Tamyeez string
	// EnglishOne and EnglishMany are the English names: "US dollar", "US dollars".
	EnglishOne, EnglishMany string
}

// Currency is a currency's two units and how many decimals the small one takes: 2 for cents, 0 for none.
type Currency struct {
	Major, Minor Noun
	MinorDigits  int
}

// USD is the US dollar and its cent.
var USD = Currency{
	Major: Noun{One: "دولار أمريكي", Two: "دولاران أمريكيان", Plural: "دولارات أمريكية", Tamyeez: "دولاراً أمريكياً",
		EnglishOne: "US dollar", EnglishMany: "US dollars"},
	Minor: Noun{One: "سنت", Two: "سنتان", Plural: "سنتات", Tamyeez: "سنتاً",
		EnglishOne: "cent", EnglishMany: "cents"},
	MinorDigits: 2,
}

// SYP is the Syrian pound — feminine, which is why it is ثلاث ليرات and not ثلاثة — and its piastre.
var SYP = Currency{
	Major: Noun{Feminine: true, One: "ليرة سورية", Two: "ليرتان سوريتان", Plural: "ليرات سورية", Tamyeez: "ليرة سورية",
		EnglishOne: "Syrian pound", EnglishMany: "Syrian pounds"},
	Minor: Noun{One: "قرش", Two: "قرشان", Plural: "قروش", Tamyeez: "قرشاً",
		EnglishOne: "piastre", EnglishMany: "piastres"},
	MinorDigits: 2,
}

// ForCode is the currency for an ISO code, and whether it is known.
func ForCode(code string) (Currency, bool) {
	switch strings.ToUpper(code) {
	case "USD":
		return USD, true
	case "SYP":
		return SYP, true
	}
	return Currency{}, false
}

// maxWhole is the largest whole part written: the billions are the last scale this package names.
const maxWhole = 999_999_999_999

// parse reads a plain decimal into its whole units and its small units.
func parse(amount string, c Currency) (whole, minor uint64, err error) {
	invalid := errs.Validation(CodeInvalid, "not an amount that can be written in words").WithParam("value", amount)
	text := strings.TrimSpace(amount)
	w, f, _ := strings.Cut(text, ".")
	if w == "" || len(f) > c.MinorDigits || !digits(w) || !digits(f) {
		return 0, 0, invalid
	}
	whole, err = strconv.ParseUint(w, 10, 64)
	if err != nil || whole > maxWhole {
		return 0, 0, invalid
	}
	if f != "" {
		f += strings.Repeat("0", c.MinorDigits-len(f))
		if minor, err = strconv.ParseUint(f, 10, 64); err != nil {
			return 0, 0, invalid
		}
	}
	return whole, minor, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ── Arabic ──────────────────────────────────────────────────────────────────────────────────────────────────────────

// Arabic writes an amount the way an invoice states it: "فقط … لا غير".
func Arabic(amount string, c Currency) (string, error) {
	whole, minor, err := parse(amount, c)
	if err != nil {
		return "", err
	}
	var parts []string
	if whole > 0 || minor == 0 {
		parts = append(parts, arabicCounted(whole, c.Major))
	}
	if minor > 0 {
		parts = append(parts, arabicCounted(minor, c.Minor))
	}
	return "فقط " + strings.Join(parts, " و") + " لا غير", nil
}

// ones are 1–10 as they count a MASCULINE noun (which takes the feminine-looking form from 3 to 10) and a feminine one.
var (
	onesForMasculine = [...]string{"", "واحد", "اثنان", "ثلاثة", "أربعة", "خمسة", "ستة", "سبعة", "ثمانية", "تسعة", "عشرة"}
	onesForFeminine  = [...]string{"", "واحدة", "اثنتان", "ثلاث", "أربع", "خمس", "ست", "سبع", "ثماني", "تسع", "عشر"}
	// In 21–99 the unit before "and twenty" keeps the counted noun's gender rule: إحدى وعشرون ليرة، ثلاث وعشرون ليرة.
	unitsInTensForFeminine = [...]string{"", "إحدى", "اثنتان", "ثلاث", "أربع", "خمس", "ست", "سبع", "ثمان", "تسع"}
	tens                   = [...]string{"", "عشرة", "عشرون", "ثلاثون", "أربعون", "خمسون", "ستون", "سبعون", "ثمانون", "تسعون"}
	hundreds               = [...]string{"", "مائة", "مائتان", "ثلاثمائة", "أربعمائة", "خمسمائة", "ستمائة", "سبعمائة", "ثمانمائة", "تسعمائة"}
)

// below100 is 1–99 counting a noun of the given gender.
func below100(n uint64, feminine bool) string {
	switch {
	case n == 0:
		return ""
	case n <= 10:
		if feminine {
			return onesForFeminine[n]
		}
		return onesForMasculine[n]
	case n == 11:
		if feminine {
			return "إحدى عشرة"
		}
		return "أحد عشر"
	case n == 12:
		if feminine {
			return "اثنتا عشرة"
		}
		return "اثنا عشر"
	case n < 20:
		// 13–19: the unit takes the opposite of the noun's gender, the ten agrees with it.
		if feminine {
			return onesForFeminine[n-10] + " عشرة"
		}
		return onesForMasculine[n-10] + " عشر"
	}
	unit, ten := n%10, n/10
	if unit == 0 {
		return tens[ten]
	}
	u := onesForMasculine[unit]
	if feminine {
		u = unitsInTensForFeminine[unit]
	}
	return u + " و" + tens[ten]
}

// below1000 is 1–999 counting a noun of the given gender. construct is set when a noun follows directly and the number
// is a round two hundred: مائتا دولار، not مائتان دولار.
func below1000(n uint64, feminine, construct bool) string {
	h, rest := n/100, n%100
	var parts []string
	if h > 0 {
		word := hundreds[h]
		if h == 2 && rest == 0 && construct {
			word = "مائتا"
		}
		parts = append(parts, word)
	}
	if rest > 0 {
		parts = append(parts, below100(rest, feminine))
	}
	return strings.Join(parts, " و")
}

// scale is a thousand, a million or a billion — itself a masculine noun counted like any other.
type scale struct {
	value                  uint64
	one, two, twoConstruct string
	plural, tamyeez        string
}

var scales = []scale{
	{value: 1_000_000_000, one: "مليار", two: "ملياران", twoConstruct: "مليارا", plural: "مليارات", tamyeez: "ملياراً"},
	{value: 1_000_000, one: "مليون", two: "مليونان", twoConstruct: "مليونا", plural: "ملايين", tamyeez: "مليوناً"},
	{value: 1_000, one: "ألف", two: "ألفان", twoConstruct: "ألفا", plural: "آلاف", tamyeez: "ألفاً"},
}

// counted is v of a scale word. construct is set when the currency follows it directly, which drops the dual's nun and
// the accusative's tanween: ألفا دولار، أحد عشر ألف دولار.
func (s scale) counted(v uint64, construct bool) string {
	switch {
	case v == 1:
		return s.one
	case v == 2:
		if construct {
			return s.twoConstruct
		}
		return s.two
	}
	words := below1000(v, false, construct && v%100 == 0)
	switch last := v % 100; {
	case last >= 3 && last <= 10:
		return words + " " + s.plural
	case last >= 11 && !construct:
		return words + " " + s.tamyeez
	default:
		return words + " " + s.one
	}
}

// arabicCounted is n of a noun: the number in words and the noun in the form the number asks for.
func arabicCounted(n uint64, noun Noun) string {
	switch n {
	case 0:
		return "صفر " + noun.One
	case 1:
		if noun.Feminine {
			return noun.One + " واحدة"
		}
		return noun.One + " واحد"
	case 2:
		return noun.Two
	}
	units := n % 1000
	var groups []string
	rest := n
	for _, s := range scales {
		if v := rest / s.value; v > 0 {
			// The last scale before the noun, with no units between them, takes the construct form.
			groups = append(groups, s.counted(v, units == 0 && rest%s.value == 0))
			rest %= s.value
		}
	}
	if units > 0 {
		groups = append(groups, below1000(units, noun.Feminine, units%100 == 0))
	}
	words := strings.Join(groups, " و")
	switch last := n % 100; {
	case last >= 3 && last <= 10:
		return words + " " + noun.Plural
	case last >= 11:
		return words + " " + noun.Tamyeez
	default:
		// A round hundred or thousand, or one that ends in 1 or 2 past them: مائة دولار، ألف وواحد دولار.
		return words + " " + noun.One
	}
}

// ── English ─────────────────────────────────────────────────────────────────────────────────────────────────────────

// English writes an amount for an English invoice: "One thousand three hundred thirty-four US dollars and fifty cents only".
func English(amount string, c Currency) (string, error) {
	whole, minor, err := parse(amount, c)
	if err != nil {
		return "", err
	}
	var parts []string
	if whole > 0 || minor == 0 {
		parts = append(parts, englishCounted(whole, c.Major))
	}
	if minor > 0 {
		parts = append(parts, englishCounted(minor, c.Minor))
	}
	text := strings.Join(parts, " and ") + " only"
	return strings.ToUpper(text[:1]) + text[1:], nil
}

var (
	englishOnes = [...]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven",
		"twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	englishTens   = [...]string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
	englishScales = []struct {
		value uint64
		name  string
	}{{1_000_000_000, "billion"}, {1_000_000, "million"}, {1_000, "thousand"}}
)

func englishBelow1000(n uint64) string {
	var parts []string
	if h := n / 100; h > 0 {
		parts = append(parts, englishOnes[h]+" hundred")
	}
	switch rest := n % 100; {
	case rest == 0:
	case rest < 20:
		parts = append(parts, englishOnes[rest])
	case rest%10 == 0:
		parts = append(parts, englishTens[rest/10])
	default:
		parts = append(parts, englishTens[rest/10]+"-"+englishOnes[rest%10])
	}
	return strings.Join(parts, " ")
}

func englishCounted(n uint64, noun Noun) string {
	name := noun.EnglishMany
	if n == 1 {
		name = noun.EnglishOne
	}
	if n == 0 {
		return "zero " + name
	}
	var parts []string
	rest := n
	for _, s := range englishScales {
		if v := rest / s.value; v > 0 {
			parts = append(parts, englishBelow1000(v)+" "+s.name)
			rest %= s.value
		}
	}
	if rest > 0 {
		parts = append(parts, englishBelow1000(rest))
	}
	return strings.Join(parts, " ") + " " + name
}
