//go:build windows

package fsutil

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func isCrossDeviceError(err error) bool {
	return errors.Is(err, syscall.EXDEV) ||
		errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
