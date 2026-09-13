package main

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// htmlMarker is the opening tag index.html ships with: Arabic, right-to-left, because Arabic is the
// product's primary language and a fresh installation's default. TestIndexHTMLCarriesTheMarker holds
// the source file to it — if the tag drifted, this middleware would silently do nothing.
const htmlMarker = `<html lang="ar" dir="rtl">`

// localeMiddleware rewrites index.html's opening tag to the stored language before the webview sees
// a single byte.
//
// # Why on the server side
//
// Setting `dir` from JavaScript happens after the first paint. On every launch an English-speaking
// shop would see a right-to-left frame, then watch the layout flip. Rewriting the document as it is
// served means the very first frame is laid out in the right direction — there is no frame before it.
func localeMiddleware(locale domain.Locale) assetserver.Middleware {
	replacement := []byte(`<html lang="` + string(locale) + `" dir="` + string(locale.Direction()) + `">`)
	marker := []byte(htmlMarker)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" && r.URL.Path != "/index.html" {
				next.ServeHTTP(w, r)
				return
			}
			buffered := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
			next.ServeHTTP(buffered, r)

			body := buffered.body.Bytes()
			if buffered.status == http.StatusOK {
				body = bytes.Replace(body, marker, replacement, 1)
			}
			for key, values := range buffered.header {
				if key == "Content-Length" {
					continue // the body length may have changed
				}
				for _, v := range values {
					w.Header().Add(key, v)
				}
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(buffered.status)
			_, _ = w.Write(body)
		})
	}
}

// bufferedResponse collects what the next handler writes, so the document can be rewritten whole.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) }
func (b *bufferedResponse) WriteHeader(status int)      { b.status = status }
