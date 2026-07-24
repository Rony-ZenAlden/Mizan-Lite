// Package buildinfo exposes version and build metadata, injected at build time via
// -ldflags. It has no dependencies and is safe to import from anywhere.
package buildinfo

import "runtime"

// These are overridden at build time, e.g.:
//
//	go build -ldflags "-X github.com/mizan-erp/mizan/internal/buildinfo.Version=1.0.0"
var (
	Version   = "0.0.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info is a serialisable snapshot of build metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Current returns the running build's metadata.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}
