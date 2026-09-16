package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/e2e"
)

func bridge(t *testing.T) (*e2e.Bridge, *httptest.Server) {
	t.Helper()
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(`<!doctype html><html lang="ar" dir="rtl"><head><title>x</title></head><body></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := e2e.New(e2e.Options{Dist: dist, Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	if err = b.Reset(context.Background(), "empty"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(b.Handler())
	t.Cleanup(server.Close)
	return b, server
}

// TestTheBridgeCallsEveryBoundMethod: every function the generated bindings export is reachable through the bridge by the name the
// frontend calls, with the envelope a direct call returns.
func TestTheBridgeCallsEveryBoundMethod(t *testing.T) {
	b, server := bridge(t)
	generated, err := filepath.Glob(filepath.Join("..", "..", "..", "apps", "lite", "frontend", "wailsjs", "go", "api", "*.d.ts"))
	if err != nil || len(generated) < 10 {
		t.Fatalf("generated bindings: %v %v — run wails generate module", generated, err)
	}
	fn := regexp.MustCompile(`export function (\w+)\(`)
	count := 0
	for _, file := range generated {
		body, _ := os.ReadFile(file)
		facade := strings.TrimSuffix(filepath.Base(file), ".d.ts")
		for _, m := range fn.FindAllStringSubmatch(string(body), -1) {
			if _, cerr := b.Call(facade, m[1], nil); cerr != nil {
				t.Errorf("%s.%s: %v", facade, m[1], cerr)
			}
			count++
		}
	}
	if count < 90 {
		t.Fatalf("only %d bound functions found", count)
	}
	resp, err := http.Post(server.URL+"/call", "application/json", strings.NewReader(`{"s":"App","m":"FirstRunStatus","args":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Complete bool `json:"complete"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&env)
	_ = resp.Body.Close()
	if !env.OK || env.Data.Complete {
		t.Fatalf("first run status through the bridge: %+v", env)
	}
	for _, bad := range []string{`{"s":"App","m":"core","args":[]}`, `{"s":"Secret","m":"X","args":[]}`, `{"s":"App","m":"About","args":[1,2]}`} {
		resp, err := http.Post(server.URL+"/call", "application/json", strings.NewReader(bad))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d", bad, resp.StatusCode)
		}
	}
}

func TestTheBridgeServesTheShellWithTheShim(t *testing.T) {
	_, server := bridge(t)
	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `window.go = { api: new Proxy`) || !strings.Contains(string(body), `<html lang="ar" dir="rtl">`) {
		t.Fatalf("index %s", body)
	}
}

func TestTheBridgeListensOnLoopbackOnly(t *testing.T) {
	b, _ := bridge(t)
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.10:34199", ":34199"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := b.Listen(ctx, addr); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Errorf("%s: %v", addr, err)
		}
	}
}
