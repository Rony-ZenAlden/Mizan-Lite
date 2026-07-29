//go:build !windows

package migrate

import "syscall"

// availableBytes reports the free space on the volume containing dir.
//
// Bavail (not Bfree) is deliberate: it is the space available to an unprivileged
// process, which is what Mizan actually is on a customer's machine. Bfree includes
// root-reserved blocks the app can never use, and trusting it would let the pre-flight
// pass and the backup then fail with a full disk — the exact outcome the check exists to
// prevent.
func availableBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	// Field widths differ across Unixes (Bsize is uint32 on darwin, int64 on linux), so
	// both operands are converted explicitly.
	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:gosec,unconvert // widths are platform-dependent
}
