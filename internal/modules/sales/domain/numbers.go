package domain

// itoa renders an int64 without importing strconv into the domain.
//
// It lived in numbering.go until Phase 6 moved the allocator to the platform; several callers in
// this package still needed it, so it moved here rather than back.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
