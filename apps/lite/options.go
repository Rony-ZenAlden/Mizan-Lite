package main

import (
	"context"
	"io/fs"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/mizan-erp/mizan/internal/lite/paths"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

// bundleID is the application's identity on both platforms.
//
// It is the macOS bundle identifier (build/darwin/Info.plist) and the single-instance lock's id, and
// it must differ from Mizan's com.mizanerp.desktop: macOS keys preferences, keychain entries and
// permissions by it, so two editions sharing one would share all of those. It must also never change
// after a release, because everything keyed to it would be orphaned.
const bundleID = "com.mizanerp.lite"

// The window's floor. The till is designed for at least this; below it the quick grid and the cart
// cannot sit side by side.
const (
	minWidth  = 1024
	minHeight = 680
)

type appConfig struct {
	Assets         fs.FS
	Paths          paths.Paths
	Locale         domain.Locale
	Version        string
	Bindings       []any
	OnStartup      func(context.Context)
	OnShutdown     func(context.Context)
	OnSecondLaunch func(options.SecondInstanceData)
}

// appOptions is the window, as data. A function rather than a literal inside main so every choice
// in it is asserted by a test on both platforms' behalf.
func appOptions(cfg appConfig) *options.App {
	return &options.App{
		Title:     "Mizan Lite",
		Width:     1280,
		Height:    800,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		// The native frame on both platforms, deliberately. A frameless window must redraw its own
		// caption buttons and drag region, mirror them for right-to-left, and re-implement Windows
		// snap behaviour — a large surface for no benefit to a shop.
		Frameless: false,
		// Painted before the webview loads, so launching does not flash white on a dark desktop. It
		// matches the light surface token in index.css.
		BackgroundColour: options.NewRGB(248, 250, 252),
		AssetServer: &assetserver.Options{
			Assets:     cfg.Assets,
			Middleware: localeMiddleware(cfg.Locale),
		},
		// A second launch focuses the running window instead of opening a second process on the same
		// database. Double-clicking a shortcut twice is ordinary on Windows, and two processes would
		// contend for the single SQLite writer and each run its own scheduled backup.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               bundleID,
			OnSecondInstanceLaunch: cfg.OnSecondLaunch,
		},
		// The browser's "Reload" and "Inspect" have no place at a till.
		EnableDefaultContextMenu: false,
		OnStartup:                cfg.OnStartup,
		OnShutdown:               cfg.OnShutdown,
		Bind:                     cfg.Bindings,
		Windows: &windows.Options{
			// Under the data directory, never beside the executable: Program Files is not writable by
			// a normal user, and WebView2's default folder there fails on first launch.
			WebviewUserDataPath:  cfg.Paths.WebView,
			WebviewIsTransparent: false,
			Theme:                windows.SystemDefault,
			// Ctrl+scroll zoom at a till changes the layout mid-sale.
			IsZoomControlEnabled: false,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarDefault(),
			About: &mac.AboutInfo{
				Title:   "Mizan Lite " + cfg.Version,
				Message: "For pantry shops",
			},
		},
	}
}
