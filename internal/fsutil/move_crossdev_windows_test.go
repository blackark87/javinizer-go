//go:build windows

package fsutil

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

func TestIsCrossDeviceError_Windows(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		wantCross bool
	}{
		{"nil_error", nil, false},
		{"generic_error", os.ErrNotExist, false},
		{"exdev", syscall.EXDEV, true},
		{"einval", syscall.EINVAL, true},
		{"error_not_same_device", windows.ERROR_NOT_SAME_DEVICE, true},
		{"link_error_not_same_device", &os.LinkError{Op: "rename", Old: "D:\\a.txt", New: "F:\\b.txt", Err: windows.ERROR_NOT_SAME_DEVICE}, true},
		{"link_error_other", &os.LinkError{Op: "rename", Old: "D:\\a.txt", New: "F:\\b.txt", Err: syscall.EACCES}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCross, isCrossDeviceError(tc.err))
		})
	}
}
