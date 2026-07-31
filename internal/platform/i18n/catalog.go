// Package i18n is the message catalog and the user-content translation resolver.
//
// Two different problems share the word "translation", and ARCHITECTURE_v1 §22.4 is explicit
// that they must not share a mechanism:
//
//   - UI strings, authored by developers, shipped in the binary  → this file (Catalog)
//   - User content, authored by the customer at runtime          → translations.go
//
// Note what this package is NOT for. The backend never produces user-facing prose (§22.2): it
// returns error codes and parameters, and the frontend renders them. That rule is what makes
// switching language complete rather than partial — translated strings coming from the server
// would leave every already-rendered error in the old language. The Catalog therefore serves
// only the few artifacts the backend renders itself: printed documents, generated file names,
// and operator-facing logs.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/locales"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeCatalogInvalid = "i18n.catalog_invalid"
	CodeWriteFailed    = "i18n.translation_write_failed"
	CodeLoadFailed     = "i18n.translation_load_failed"
	CodeInvalidLocale  = "i18n.invalid_locale"
)

// Catalog holds every UI message, by locale.
type Catalog struct {
	messages map[locale.Locale]map[string]string
}

// Load parses the embedded catalogs.
func Load() (*Catalog, error) { return LoadFS(locales.FS()) }

// LoadFS parses catalogs from any filesystem, so tests can supply their own.
//
// A malformed catalog is a hard failure, unlike an unknown settings row (0.5, D3). The
// distinction matters: a stale settings row is inert, whereas a catalog that will not parse
// silently blanks the entire user interface. This is a build defect that must never reach a
// customer, so it fails at startup on a developer's machine.
func LoadFS(fsys fs.FS) (*Catalog, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeCatalogInvalid,
			"reading the locales directory")
	}

	c := &Catalog{messages: map[locale.Locale]map[string]string{}}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Locales are discovered from the directory listing, never enumerated in code.
		loc, ok := locale.Parse(entry.Name())
		if !ok {
			return nil, errs.Validation(CodeCatalogInvalid,
				"locales contains a directory that is not a valid locale tag").
				WithParam("name", entry.Name())
		}
		messages, err := loadLocale(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
		c.messages[loc] = messages
	}
	if len(c.messages) == 0 {
		return nil, errs.Internal(CodeCatalogInvalid, "no locales were found")
	}
	if _, ok := c.messages[locale.Default]; !ok {
		// Every fallback chain ends at Default, so without it a missing key in any locale
		// would have nowhere to fall back to.
		return nil, errs.Internal(CodeCatalogInvalid,
			"the default locale has no catalog").WithParam("locale", locale.Default.String())
	}
	return c, nil
}

func loadLocale(fsys fs.FS, dir string) (map[string]string, error) {
	files, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeCatalogInvalid,
			"reading locale directory "+dir)
	}

	out := map[string]string{}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		name := path.Join(dir, f.Name())
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeCatalogInvalid,
				"reading catalog "+name)
		}
		var messages map[string]string
		if err := json.Unmarshal(raw, &messages); err != nil {
			return nil, errs.Wrap(err, errs.CategoryValidation, CodeCatalogInvalid,
				"catalog "+name+" is not valid JSON")
		}
		for key, value := range messages {
			if existing, dup := out[key]; dup && existing != value {
				// Two files in one locale defining the same key differently: whichever won
				// would depend on directory order, so neither is allowed to.
				return nil, errs.Validation(CodeCatalogInvalid,
					"key is defined twice in the same locale").
					WithParam("locale", dir).WithParam("key", key)
			}
			out[key] = value
		}
	}
	return out, nil
}

// T resolves a key for a locale, interpolating params.
//
// It cannot fail. Resolution walks the locale's fallback chain (ar-SY → ar → en) and, if no
// locale defines the key, RETURNS THE KEY ITSELF.
//
// Returning the key rather than an empty string is deliberate: a missing translation shows
// "common.action.save" on screen — ugly, obviously wrong, and instantly greppable. A blank
// button is none of those things, and would be reported as "the save button disappeared".
func (c *Catalog) T(l locale.Locale, key string, params map[string]string) string {
	for _, candidate := range l.FallbackChain() {
		if messages, ok := c.messages[candidate]; ok {
			if template, found := messages[key]; found {
				return interpolate(template, params)
			}
		}
	}
	return key
}

// Has reports whether the locale itself defines the key, without falling back. The
// completeness gates use this; ordinary callers want T.
func (c *Catalog) Has(l locale.Locale, key string) bool {
	messages, ok := c.messages[l]
	if !ok {
		return false
	}
	_, found := messages[key]
	return found
}

// Locales returns every loaded locale, sorted.
func (c *Catalog) Locales() []locale.Locale {
	out := make([]locale.Locale, 0, len(c.messages))
	for l := range c.messages {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Keys returns every key defined by a locale, sorted.
func (c *Catalog) Keys(l locale.Locale) []string {
	messages, ok := c.messages[l]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(messages))
	for k := range messages {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// interpolate replaces {name} placeholders.
//
// Plain brace substitution rather than text/template: catalogs are edited by translators, and
// a template syntax error would become a runtime failure inside a string a non-programmer
// wrote. An unknown placeholder is left visible rather than dropped or erroring — a stray
// "{version}" on screen is a bug report; a silently missing value is a mystery.
func interpolate(template string, params map[string]string) string {
	if len(params) == 0 || !strings.ContainsRune(template, '{') {
		return template
	}
	var b strings.Builder
	b.Grow(len(template))

	for i := 0; i < len(template); {
		open := strings.IndexByte(template[i:], '{')
		if open < 0 {
			b.WriteString(template[i:])
			break
		}
		open += i
		closeIdx := strings.IndexByte(template[open:], '}')
		if closeIdx < 0 {
			b.WriteString(template[i:])
			break
		}
		closeIdx += open

		b.WriteString(template[i:open])
		name := template[open+1 : closeIdx]
		if value, ok := params[name]; ok {
			b.WriteString(value)
		} else {
			b.WriteString(template[open : closeIdx+1]) // leave it visible
		}
		i = closeIdx + 1
	}
	return b.String()
}

// Param is a convenience for building the params map at a call site.
func Param(pairs ...string) map[string]string {
	if len(pairs)%2 != 0 {
		return nil
	}
	out := make(map[string]string, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out[pairs[i]] = pairs[i+1]
	}
	return out
}

// TranslateError renders a typed error for a locale, using its Code as the catalog key and
// its Params for interpolation.
//
// This is the bridge for the backend-rendered artifacts named in the package comment. It does
// NOT change the rule that the API layer returns codes rather than prose.
func (c *Catalog) TranslateError(l locale.Locale, err error) string {
	if err == nil {
		return ""
	}
	e, ok := errs.AsError(err)
	if !ok {
		return fmt.Sprint(err)
	}
	return c.T(l, e.Code, e.Params)
}
