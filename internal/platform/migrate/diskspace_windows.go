//go:build windows

package migrate

import (
	"syscall"
	"unsafe"
)

// GetDiskFreeSpaceExW is resolved lazily from kernel32 rather than pulling in
// golang.org/x/sys, keeping the offline dependency surface at stdlib only.
var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// availableBytes reports the free space on the volume containing dir.
//
// The first out-parameter (lpFreeBytesAvailableToCaller) is the one used: it accounts
// for per-user disk quotas, so it is the space this process can actually write, which is
// the only number the pre-flight check may rely on.
func availableBytes(dir string) (uint64, error) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var availableToCaller, totalBytes, totalFree uint64
	// unsafe.Pointer is required by the syscall ABI; the variables outlive the call.
	ret, _, callErr := getDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&availableToCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return 0, callErr
	}
	return availableToCaller, nil
}
