package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mizan-erp/mizan/internal/lite/api"
)

// wailsFiles is the operating system's file dialogs, through Wails, for exports and backups (L7 §4.4). The context each call
// receives is the window's (api.Set.SetContext).
type wailsFiles struct{}

func filters(in []api.Filter) []wailsruntime.FileFilter {
	out := make([]wailsruntime.FileFilter, 0, len(in))
	for _, f := range in {
		out = append(out, wailsruntime.FileFilter{DisplayName: f.Name + " (" + f.Pattern + ")", Pattern: f.Pattern})
	}
	return out
}

func (wailsFiles) SaveFile(ctx context.Context, title, defaultName string, in []api.Filter) (string, error) {
	return wailsruntime.SaveFileDialog(ctx, wailsruntime.SaveDialogOptions{Title: title, DefaultFilename: defaultName, Filters: filters(in),
		CanCreateDirectories: true})
}

func (wailsFiles) OpenFile(ctx context.Context, title string, in []api.Filter) (string, error) {
	return wailsruntime.OpenFileDialog(ctx, wailsruntime.OpenDialogOptions{Title: title, Filters: filters(in)})
}

func (wailsFiles) PickFolder(ctx context.Context, title string) (string, error) {
	return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
}

// ShowInFolder opens the system's file manager with the file selected.
func (wailsFiles) ShowInFolder(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}
