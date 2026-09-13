// Package textkey reduces text to the form Mizan Lite compares and searches: one key for every
// spelling a reader would call the same word.
//
// # Why
//
// أسطنبولي, إسطنبولي and اسطنبولي are one word to a reader and three strings to a database. So are زيت
// and زيـــت (tatweel), رمانة and رمانه (ta marbuta), حلوى and حلوي (alef maqsura). A search that compares
// bytes finds none of the other spellings, and a uniqueness check that compares bytes lets a shop create
// the same product three times — the duplicate nobody spots until a stock count.
//
// Only Go normalises: search runs in Go, so there is one implementation (L1 §5, D-L1.3).
package textkey

import (
	"strings"
	"unicode"
)

// Normalise returns the comparison key for s. It is idempotent and never lengthens its input.
//
// What it deliberately does NOT do: remove the definite article ال. الزيت and زيت are one word to a
// reader, but stripping ال also mangles words that merely begin with those letters (الماس → ماس), and
// substring search already finds زيت inside الزيت.
func Normalise(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range s {
		folded, keep := fold(r)
		if !keep {
			continue
		}
		if isSpace(folded) {
			// Runs of any whitespace collapse to one space, and leading/trailing space is dropped.
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(folded)
	}
	return b.String()
}

// fold maps one rune to its key form, or reports that it carries no meaning for comparison.
func fold(r rune) (rune, bool) {
	switch {
	// Harakat (fatha, damma, kasra, sukun, shadda, tanwin, and the extended marks) and the superscript
	// alef: pronunciation guides a reader skips.
	case r >= 0x064B && r <= 0x065F, r == 0x0670:
		return 0, false
	case r == 0x0640: // tatweel, a stretching character
		return 0, false
	case r == 0x200C, r == 0x200D, r == 0x200E, r == 0x200F, r == 0xFEFF:
		// Zero-width joiners, direction marks and the byte-order mark: invisible, and pasted with names.
		return 0, false
	case r == 'أ', r == 'إ', r == 'آ', r == 'ٱ':
		return 'ا', true
	case r == 'ى':
		return 'ي', true
	case r == 'ة':
		return 'ه', true
	case r == 'ؤ':
		return 'و', true
	case r == 'ئ':
		return 'ي', true
	case r == 'ک': // Persian kaf
		return 'ك', true
	case r == 'ی': // Persian yeh
		return 'ي', true
	case r >= '٠' && r <= '٩': // Arabic-Indic digits
		return '0' + (r - '٠'), true
	case r >= '۰' && r <= '۹': // Extended Arabic-Indic (Persian) digits
		return '0' + (r - '۰'), true
	case r == 0x00A0: // no-break space
		return ' ', true
	}
	return unicode.ToLower(r), true
}

func isSpace(r rune) bool { return unicode.IsSpace(r) }
