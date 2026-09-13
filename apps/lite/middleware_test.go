package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

const document = `<!doctype html>
<html lang="ar" dir="rtl">
  <head><title>x</title></head><body></body>
</html>`

func serve(t *testing.T, locale domain.Locale, path string, upstream http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	handler := localeMiddleware(locale)(upstream)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func html(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}
}

func TestTheDocumentIsServedInTheStoredLanguage(t *testing.T) {
	for _, path := range []string{"/", "/index.html"} {
		t.Run("english at "+path, func(t *testing.T) {
			rec := serve(t, domain.English, path, html(document))
			body := rec.Body.String()
			if !strings.Contains(body, `<html lang="en" dir="ltr">`) {
				t.Fatalf("body = %s", body)
			}
			if strings.Contains(body, htmlMarker) {
				t.Fatal("the Arabic tag survived alongside the English one")
			}
			if rec.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
				t.Fatalf("Content-Length %q does not match the rewritten body (%d bytes)",
					rec.Header().Get("Content-Length"), len(body))
			}
			if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("headers were not carried: %v", rec.Header())
			}
		})
	}

	t.Run("arabic keeps the tag", func(t *testing.T) {
		if body := serve(t, domain.Arabic, "/", html(document)).Body.String(); !strings.Contains(body, htmlMarker) {
			t.Fatalf("body = %s", body)
		}
	})
}

func TestOtherAssetsPassThroughUntouched(t *testing.T) {
	script := `console.log('<html lang="ar" dir="rtl">')`
	rec := serve(t, domain.English, "/assets/index-abc.js", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(script))
	})
	if rec.Body.String() != script {
		t.Fatalf("a script was rewritten: %s", rec.Body.String())
	}
}

func TestANonOKDocumentIsNotRewritten(t *testing.T) {
	rec := serve(t, domain.English, "/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(document))
	})
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), htmlMarker) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

// TestIndexHTMLCarriesTheMarker holds the source document to the exact tag the middleware looks
// for. Without it, a reformatted index.html would make the middleware a silent no-op and every
// English launch would flash right-to-left again — with every other test still green.
func TestIndexHTMLCarriesTheMarker(t *testing.T) {
	for _, file := range []string{
		filepath.Join("frontend", "index.html"),
		filepath.Join("frontend", "dist", "index.html"),
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if n := strings.Count(string(raw), htmlMarker); n != 1 {
			t.Errorf("%s contains the marker %d times, want exactly 1", file, n)
		}
	}
}
