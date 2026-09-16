// Package e2e is the end-to-end bridge (L8 D-L8.1): the built frontend served to a real browser, its `window.go.api` calls
// answered by the very api.Set the application binds, attached to a real graph on a real data directory. What the Wails window
// does with IPC, this does with one HTTP call per binding — so a journey clicks through the real screens against the real Go,
// in Chrome on a Mac and in Edge (WebView2's engine) on Windows.
//
// It exists only for tests: cmd/lite-e2e runs it, lite-e2e-test-only forbids everything else importing it, and it listens on the
// loopback address only. The shipped application never contains it.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/demoseed"
	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/printers"
)

// SeedPIN is the seeded shop's owner PIN.
const SeedPIN = "481537"

// shim defines window.go.api as the generated bindings expect it: every call becomes POST /call and resolves to the envelope.
const shim = `<script>
(function () {
  const call = (s, m) => async (...args) => {
    const r = await fetch("/call", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ s, m, args }) });
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  };
  const facade = (s) => new Proxy({}, { get: (_, m) => call(s, String(m)) });
  window.go = { api: new Proxy({}, { get: (_, s) => facade(String(s)) }) };
})();
</script>`

// Options configures a bridge.
type Options struct {
	// Dist is the built frontend (apps/lite/frontend/dist).
	Dist string
	// Root is a directory the bridge owns: shops, saved files, the seeded template.
	Root   string
	Logger *slog.Logger
}

// Bridge serves the frontend and one graph at a time.
type Bridge struct {
	opts Options
	set  *api.Set

	mu      sync.Mutex
	app     *bootstrap.App
	dataDir string
	shops   int
	files   *Files
	printer *Printer
	seeded  string // a seeded shop's database, copied for each reset
	bound   map[string]reflect.Value
}

// New builds a bridge with no shop yet; Reset makes one.
func New(opts Options) (*Bridge, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if _, err := os.Stat(filepath.Join(opts.Dist, "index.html")); err != nil {
		return nil, fmt.Errorf("e2e: the built frontend is missing (%w) — run npm run build", err)
	}
	b := &Bridge{opts: opts, set: api.New("0.0.0-e2e", opts.Logger), files: &Files{}, printer: &Printer{}}
	b.set.SetContext(context.Background())
	b.set.SetFiles(b.files)
	b.set.SetPrinters(b.printer)
	b.set.SetRestarter(b.restart)
	b.bound = map[string]reflect.Value{}
	for _, facade := range b.set.Bindings() {
		b.bound[reflect.TypeOf(facade).Elem().Name()] = reflect.ValueOf(facade)
	}
	return b, nil
}

// Reset replaces the shop: "empty" is a fresh installation (first run), "seeded" the demo shop (PIN 481537). The saved files
// and printed jobs are forgotten.
func (b *Bridge) Reset(ctx context.Context, fixture string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked(ctx)
	b.shops++
	b.dataDir = filepath.Join(b.opts.Root, fmt.Sprintf("shop-%d", b.shops))
	b.files.reset(filepath.Join(b.opts.Root, fmt.Sprintf("saved-%d", b.shops)))
	b.printer.reset()
	p := paths.Layout(b.dataDir)
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	switch fixture {
	case "empty":
	case "seeded":
		if err := b.ensureSeededLocked(ctx); err != nil {
			return err
		}
		body, err := os.ReadFile(b.seeded)
		if err != nil {
			return err
		}
		if err = os.WriteFile(p.DBFile, body, 0o600); err != nil {
			return err
		}
	default:
		return fmt.Errorf("e2e: unknown fixture %q", fixture)
	}
	return b.startLocked(ctx)
}

// ensureSeededLocked seeds the demo shop once and keeps its database for every later reset.
func (b *Bridge) ensureSeededLocked(ctx context.Context) error {
	if b.seeded != "" {
		return nil
	}
	p := paths.Layout(filepath.Join(b.opts.Root, "seed-template"))
	for _, dir := range []string{p.Data, p.Backups, p.Logs, p.WebView} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: p, Logger: b.opts.Logger})
	if err != nil {
		return err
	}
	if _, err = demoseed.Run(ctx, app, demoseed.Options{PIN: SeedPIN, Locale: "ar"}); err != nil {
		_ = app.Shutdown(ctx)
		return err
	}
	if err = app.Shutdown(ctx); err != nil {
		return err
	}
	b.seeded = p.DBFile
	return nil
}

func (b *Bridge) startLocked(ctx context.Context) error {
	app, err := bootstrap.Start(ctx, bootstrap.Options{Paths: paths.Layout(b.dataDir), Logger: b.opts.Logger})
	if err != nil {
		return err
	}
	b.app = app
	b.set.Attach(app)
	return nil
}

func (b *Bridge) closeLocked(ctx context.Context) {
	if b.app == nil {
		return
	}
	b.set.Detach()
	_ = b.app.Shutdown(ctx)
	b.app = nil
}

// restart is what the shell does after a restore: close the graph and start it again on the same data directory, which applies
// the staged file.
func (b *Bridge) restart() {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx := context.Background()
	b.closeLocked(ctx)
	if err := b.startLocked(ctx); err != nil {
		b.opts.Logger.Error("e2e restart failed", slog.Any("error", err))
	}
}

// Close stops the graph.
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked(context.Background())
}

// Call invokes a bound method by name with JSON arguments, exactly as Wails would, and returns its envelope as JSON.
func (b *Bridge) Call(facade, method string, args []json.RawMessage) ([]byte, error) {
	value, ok := b.bound[facade]
	if !ok {
		return nil, fmt.Errorf("e2e: %s is not bound", facade)
	}
	m := value.MethodByName(method)
	if !m.IsValid() || !isExported(method) {
		return nil, fmt.Errorf("e2e: %s.%s is not bound", facade, method)
	}
	if len(args) > m.Type().NumIn() {
		return nil, fmt.Errorf("e2e: %s.%s takes %d arguments, got %d", facade, method, m.Type().NumIn(), len(args))
	}
	in := make([]reflect.Value, m.Type().NumIn())
	for i := range in {
		arg := reflect.New(m.Type().In(i))
		if i < len(args) {
			if err := json.Unmarshal(args[i], arg.Interface()); err != nil {
				return nil, fmt.Errorf("e2e: %s.%s argument %d: %w", facade, method, i+1, err)
			}
		}
		in[i] = arg.Elem()
	}
	out := m.Call(in)
	if len(out) != 1 {
		return nil, fmt.Errorf("e2e: %s.%s returns %d values", facade, method, len(out))
	}
	return json.Marshal(out[0].Interface())
}

func isExported(name string) bool { return name != "" && strings.ToUpper(name[:1]) == name[:1] }

// Handler serves the frontend, the bindings and the test controls.
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			S    string            `json:"s"`
			M    string            `json:"m"`
			Args []json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body, err := b.Call(req.S, req.M, req.Args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("POST /__e2e/reset", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Fixture string `json:"fixture"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := b.Reset(r.Context(), req.Fixture); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"dataDir": b.dataDir, "saveDir": b.files.dir()})
	})
	mux.HandleFunc("POST /__e2e/files", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Open   string `json:"open"`
			Folder string `json:"folder"`
			Cancel bool   `json:"cancel"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		b.files.set(req.Open, req.Folder, req.Cancel)
		writeJSON(w, map[string]string{"saveDir": b.files.dir()})
	})
	mux.HandleFunc("GET /__e2e/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"saved": b.files.savedPaths(), "printed": b.printer.count(), "dataDir": b.dataDir})
	})
	static := http.FileServer(http.Dir(b.opts.Dist))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			static.ServeHTTP(w, r)
			return
		}
		b.serveIndex(w) //nolint:contextcheck // the bindings answer on the Set's own context, as they do under Wails
	})
	return mux
}

// serveIndex writes index.html as the application's middleware does — the stored language in <html lang dir> before the first
// paint — with the bridge's shim ahead of the bundle.
func (b *Bridge) serveIndex(w http.ResponseWriter) {
	raw, err := os.ReadFile(filepath.Join(b.opts.Dist, "index.html"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	locale, dir := "ar", "rtl"
	if s := b.set.Settings.Get(); s.OK && s.Data.Locale == "en" {
		locale, dir = "en", "ltr"
	}
	raw = bytes.Replace(raw, []byte(`<html lang="ar" dir="rtl">`), []byte(`<html lang="`+locale+`" dir="`+dir+`">`), 1)
	raw = bytes.Replace(raw, []byte("<head>"), []byte("<head>"+shim), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Listen serves on a loopback address until ctx ends. An address that is not loopback is refused.
func (b *Bridge) Listen(ctx context.Context, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("e2e: %s is not a loopback address", addr)
	}
	server := &http.Server{Addr: addr, Handler: b.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Files stands in for the system's dialogs: a Save writes under the bridge's own folder, an Open answers what the test set.
type Files struct {
	mu     sync.Mutex
	saveTo string
	open   string
	folder string
	cancel bool
	saved  []string
}

func (f *Files) reset(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveTo, f.open, f.folder, f.cancel, f.saved = dir, "", "", false, nil
	_ = os.MkdirAll(dir, 0o700)
}

func (f *Files) set(open, folder string, cancel bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open, f.folder, f.cancel = open, folder, cancel
}

func (f *Files) dir() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveTo
}

func (f *Files) savedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.saved...)
}

// SaveFile answers a path in the bridge's folder under the proposed name.
func (f *Files) SaveFile(_ context.Context, _, name string, _ []api.Filter) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel {
		return "", nil
	}
	path := filepath.Join(f.saveTo, filepath.Base(name))
	f.saved = append(f.saved, path)
	return path, nil
}

// OpenFile answers the file the test set.
func (f *Files) OpenFile(context.Context, string, []api.Filter) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel {
		return "", nil
	}
	return f.open, nil
}

// PickFolder answers the folder the test set.
func (f *Files) PickFolder(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.folder, nil
}

// ShowInFolder does nothing a browser could see.
func (f *Files) ShowInFolder(string) error { return nil }

// Printer is a receipt printer that accepts every job.
type Printer struct {
	mu   sync.Mutex
	jobs []printers.Job
}

func (p *Printer) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = nil
}

func (p *Printer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.jobs)
}

// List names the printer the seeded shop is set up with.
func (p *Printer) List(context.Context) ([]printers.Printer, error) {
	return []printers.Printer{{Name: demoseed.DemoPrinter, Default: true}}, nil
}

// Send records the job.
func (p *Printer) Send(_ context.Context, job printers.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = append(p.jobs, job)
	return nil
}
