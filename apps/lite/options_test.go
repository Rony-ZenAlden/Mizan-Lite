package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

func TestAppOptions(t *testing.T) {
	p := paths.Layout(`C:\Users\محمد\AppData\Local\Mizan Lite`)
	bindings := []any{&struct{}{}}
	started, stopped, second := false, false, false
	opts := appOptions(appConfig{
		Assets:         fstest.MapFS{},
		Paths:          p,
		Locale:         domain.English,
		Version:        "9.9.9",
		Bindings:       bindings,
		OnStartup:      func(context.Context) { started = true },
		OnShutdown:     func(context.Context) { stopped = true },
		OnSecondLaunch: func(options.SecondInstanceData) { second = true },
	})

	if opts.Title != "Mizan Lite" {
		t.Errorf("Title = %q", opts.Title)
	}
	if opts.MinWidth != minWidth || opts.MinHeight != minHeight || opts.Width < opts.MinWidth || opts.Height < opts.MinHeight {
		t.Errorf("size %dx%d with floor %dx%d", opts.Width, opts.Height, opts.MinWidth, opts.MinHeight)
	}
	if opts.Frameless {
		t.Error("the window must keep the native frame on both platforms")
	}
	if opts.BackgroundColour == nil {
		t.Error("no background colour: launching would flash white")
	}
	if opts.EnableDefaultContextMenu {
		t.Error("the browser context menu (Reload, Inspect) must be off")
	}

	if opts.SingleInstanceLock == nil || opts.SingleInstanceLock.UniqueId != bundleID {
		t.Fatalf("SingleInstanceLock = %+v, want the bundle id", opts.SingleInstanceLock)
	}
	if bundleID == "com.mizanerp.desktop" {
		t.Error("Lite must not share Mizan's identity")
	}

	if opts.AssetServer == nil || opts.AssetServer.Middleware == nil || opts.AssetServer.Assets == nil {
		t.Fatalf("AssetServer = %+v, want assets and the locale middleware", opts.AssetServer)
	}
	if len(opts.Bind) != 1 || opts.Bind[0] != bindings[0] {
		t.Errorf("Bind = %v, want exactly the bindings handed in", opts.Bind)
	}

	if opts.Windows == nil {
		t.Fatal("no Windows options")
	}
	if opts.Windows.WebviewUserDataPath != p.WebView {
		t.Errorf("WebviewUserDataPath = %q, want the data directory's %q", opts.Windows.WebviewUserDataPath, p.WebView)
	}
	if opts.Windows.IsZoomControlEnabled {
		t.Error("zoom must be off at a till")
	}
	if opts.Windows.Theme != windows.SystemDefault {
		t.Errorf("Windows theme = %v", opts.Windows.Theme)
	}
	if opts.Mac == nil || opts.Mac.TitleBar == nil || opts.Mac.About == nil {
		t.Fatalf("Mac options = %+v", opts.Mac)
	}

	opts.OnStartup(context.Background())
	opts.OnShutdown(context.Background())
	opts.SingleInstanceLock.OnSecondInstanceLaunch(options.SecondInstanceData{})
	if !started || !stopped || !second {
		t.Errorf("lifecycle hooks not wired: startup %v, shutdown %v, second launch %v", started, stopped, second)
	}
}
