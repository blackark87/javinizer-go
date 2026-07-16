package ssrf

import "net"

// SetLookupIPForTest overrides the package's IP lookup function for the duration of a test, returning a restore function.
func SetLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	return setLookupIPForTest(fn)
}
