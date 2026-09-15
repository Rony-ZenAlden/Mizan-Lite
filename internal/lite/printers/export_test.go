//go:build !windows

package printers

import "context"

// NewForTest is CUPS printing with commands answered by run, writing job files in dir.
func NewForTest(run func(ctx context.Context, name string, args ...string) ([]byte, error), dir string) System {
	return &cups{run: run, dir: dir}
}
