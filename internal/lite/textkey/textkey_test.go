package textkey_test

import (
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/lite/textkey"
)

// One fixed example per rule in L1 §5, so a rule dropped from the implementation fails by name.
func TestEachRule(t *testing.T) {
	cases := []struct {
		rule, in, want string
	}{
		{"harakat are removed", "زَيْتٌ", "زيت"},
		{"shadda and sukun are removed", "رُمّانْ", "رمان"},
		{"the superscript alef is removed", "هٰذا", "هذا"},
		{"tatweel is removed", "زيـــت", "زيت"},
		{"alef with hamza above", "أسطنبولي", "اسطنبولي"},
		{"alef with hamza below", "إسطنبولي", "اسطنبولي"},
		{"alef with madda", "آذار", "اذار"},
		{"alef wasla", "ٱلزيت", "الزيت"},
		{"alef maqsura", "حلوى", "حلوي"},
		{"ta marbuta", "رمانة", "رمانه"},
		{"hamza on waw", "مؤونة", "موونه"}, // م ؤ و ن ة → م و و ن ه: the waw it sits on stays
		{"hamza on ya", "بيئة", "بييه"},
		{"Persian kaf and yeh", "کشمش ی", "كشمش ي"},
		{"Arabic-Indic digits", "زيت ١٦ لتر", "زيت 16 لتر"},
		{"Extended Arabic-Indic digits", "۱۶", "16"},
		{"Latin is lower-cased", "Olive OIL", "olive oil"},
		{"whitespace runs collapse", "زيت \t\n  زيتون", "زيت زيتون"},
		{"surrounding whitespace is trimmed", "  زيت  ", "زيت"},
		{"a no-break space is a space", "زيت\u00a0زيتون", "زيت زيتون"},
		{"zero-width and direction marks are removed", "زي\u200cت\u200f", "زيت"},
		{"empty stays empty", "", ""},
		{"only marks becomes empty", "ـــَ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			if got := textkey.Normalise(tc.in); got != tc.want {
				t.Fatalf("Normalise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSpellingsOfOneWordShareAKey(t *testing.T) {
	// The case the package exists for: three spellings a shop would type for one product.
	a, b, c := textkey.Normalise("أسطنبولي"), textkey.Normalise("إسطنبولي"), textkey.Normalise("اسطنبولي")
	if a != b || b != c {
		t.Fatalf("keys differ: %q %q %q", a, b, c)
	}
}

func TestTheDefiniteArticleIsKept(t *testing.T) {
	// Stripping ال would turn الماس (diamond) into ماس. Substring search finds زيت in الزيت already.
	if got := textkey.Normalise("الماس"); got != "الماس" {
		t.Fatalf("got %q", got)
	}
}

// arabicish draws strings mixing every character class the rules touch, so the properties are tested
// where they could plausibly break rather than on ASCII.
func arabicish() *rapid.Generator[string] {
	alphabet := []rune("ابتثجحخدذرزسشصضطظعغفقكلمنهويأإآٱىةؤئکی٠١٢٣٤٥٦٧٨٩۰۱۲۳abcXYZ019 \t\u00a0\u200c\u200fـًٌٍَُِّْٰ")
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(0, 40).Draw(t, "n")
		out := make([]rune, n)
		for i := range out {
			out[i] = alphabet[rapid.IntRange(0, len(alphabet)-1).Draw(t, "i")]
		}
		return string(out)
	})
}

func TestNormaliseIsIdempotent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		s := arabicish().Draw(rt, "s")
		once := textkey.Normalise(s)
		if twice := textkey.Normalise(once); twice != once {
			rt.Fatalf("Normalise(Normalise(%q)) = %q, want %q", s, twice, once)
		}
	})
}

func TestNormaliseNeverLengthens(t *testing.T) {
	// A key longer than its source could overflow the column the source fitted in.
	rapid.Check(t, func(rt *rapid.T) {
		s := arabicish().Draw(rt, "s")
		if got := textkey.Normalise(s); utf8.RuneCountInString(got) > utf8.RuneCountInString(s) || len(got) > len(s) {
			rt.Fatalf("Normalise(%q) = %q is longer", s, got)
		}
	})
}
