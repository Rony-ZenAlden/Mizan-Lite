package match

import "testing"

func TestGlob(t *testing.T) {
	mod := "github.com/mizan-erp/mizan"
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// trailing /** matches the base package AND subpackages
		{mod + "/internal/modules/*/domain/**", mod + "/internal/modules/sales/domain", true},
		{mod + "/internal/modules/*/domain/**", mod + "/internal/modules/sales/domain/vo", true},
		{mod + "/internal/modules/*/domain/**", mod + "/internal/modules/sales/app", false},
		// single * stays within one segment
		{mod + "/internal/modules/*/domain/**", mod + "/internal/modules/a/b/domain", false},
		// kernel purity
		{mod + "/internal/kernel/**", mod + "/internal/kernel/money", true},
		{mod + "/internal/kernel/**", mod + "/internal/platform/database", false},
		// file-path globs (used by forbid-call excludes)
		{"**_test.go", "/x/y/foo_test.go", true},
		{"**_test.go", "/x/y/foo.go", false},
		{"**/cmd/**", "/repo/cmd/mizan/main.go", true},
		{"**/bootstrap/**", "/repo/internal/bootstrap/wire.go", true},
		{"**/bootstrap/**", "/repo/internal/modules/sales/app.go", false},
		// ? matches one non-slash char
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
	}
	for _, c := range cases {
		if got := Glob(c.pattern, c.path); got != c.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestIsStd(t *testing.T) {
	std := []string{"fmt", "os", "database/sql", "net/http"}
	nonStd := []string{"github.com/google/uuid", "modernc.org/sqlite", "gopkg.in/yaml.v3"}
	for _, p := range std {
		if !IsStd(p) {
			t.Errorf("IsStd(%q) = false, want true", p)
		}
	}
	for _, p := range nonStd {
		if IsStd(p) {
			t.Errorf("IsStd(%q) = true, want false", p)
		}
	}
}
