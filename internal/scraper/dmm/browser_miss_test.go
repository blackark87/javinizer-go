package dmm

import (
	"context"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- validateBrowserURL: additional edge cases not in browser_test.go ---

func TestValidateBrowserURL_ZeroPort(t *testing.T) {
	err := validateBrowserURL("http://localhost:0/path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestValidateBrowserURL_ValidPort8080(t *testing.T) {
	err := validateBrowserURL("http://localhost:8080/path")
	assert.NoError(t, err)
}

func TestValidateBrowserURL_Port1(t *testing.T) {
	err := validateBrowserURL("http://localhost:1/path")
	assert.NoError(t, err)
}

func TestValidateBrowserURL_Port65535(t *testing.T) {
	err := validateBrowserURL("http://localhost:65535/path")
	assert.NoError(t, err)
}

func TestValidateBrowserURL_NonNumericPort(t *testing.T) {
	err := validateBrowserURL("http://localhost:abc/path")
	require.Error(t, err)
}

func TestValidateBrowserURL_FTPSchemeRejected(t *testing.T) {
	err := validateBrowserURL("ftp://example.com/file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
}

func TestValidateBrowserURL_FileSchemeRejected(t *testing.T) {
	err := validateBrowserURL("file:///tmp/test.html")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
}

func TestValidateBrowserURL_IPv4WithPort(t *testing.T) {
	err := validateBrowserURL("http://192.168.1.1:8080/page")
	assert.NoError(t, err)
}

// --- system.IsRunningInContainer: env var combinations ---

func TestIsRunningInContainer_ChromeEnvsAreNotAContainerSignal(t *testing.T) {
	// CHROME_BIN/CHROME_PATH point at the Chrome binary location and are set
	// by users on normal hosts too; they must NOT be treated as a container
	// signal, otherwise a host with CHROME_BIN set would be misdetected as a
	// container and have Chrome's sandbox disabled. Container detection must
	// rely on /.dockerenv or /proc/1/cgroup instead.
	origChromeBin := os.Getenv("CHROME_BIN")
	origChromePath := os.Getenv("CHROME_PATH")
	os.Setenv("CHROME_BIN", "/usr/bin/chromium")
	os.Setenv("CHROME_PATH", "/usr/bin/google-chrome")
	defer func() {
		if origChromeBin != "" {
			os.Setenv("CHROME_BIN", origChromeBin)
		} else {
			os.Unsetenv("CHROME_BIN")
		}
		if origChromePath != "" {
			os.Setenv("CHROME_PATH", origChromePath)
		} else {
			os.Unsetenv("CHROME_PATH")
		}
	}()

	result := system.IsRunningInContainer(afero.NewMemMapFs())
	assert.False(t, result, "CHROME_BIN/CHROME_PATH must not imply a container")
}

// --- fetchWithBrowser: proxy profile code paths ---

func TestFetchWithBrowser_WithProxyProfileWithCredentials(t *testing.T) {
	proxyProfile := &models.ProxyProfile{
		URL:      "http://proxy.example.com:8080",
		Username: "user",
		Password: "pass",
	}

	// Should not panic; will fail because no Chrome is available
	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, proxyProfile, os.Getenv, afero.NewOsFs())
	_ = err
}

func TestFetchWithBrowser_WithProxyProfileNoCredentials(t *testing.T) {
	proxyProfile := &models.ProxyProfile{
		URL: "http://proxy.example.com:8080",
	}

	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, proxyProfile, os.Getenv, afero.NewOsFs())
	_ = err
}

func TestFetchWithBrowser_ProxyProfileMissingScheme(t *testing.T) {
	proxyProfile := &models.ProxyProfile{
		URL:      "proxy-no-scheme:8080",
		Username: "user",
		Password: "pass",
	}

	// This tests the code path where proxy URL doesn't have "://" separator
	// The warn path should be hit but the function should continue
	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, proxyProfile, os.Getenv, afero.NewOsFs())
	_ = err
}

func TestFetchWithBrowser_NilProxyProfile(t *testing.T) {
	// Should not panic with nil proxy profile
	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, nil, os.Getenv, afero.NewOsFs())
	_ = err
}

// --- fetchWithBrowser: context cancellation ---

func TestFetchWithBrowser_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchWithBrowser(ctx, "https://www.dmm.co.jp/", 5, nil, os.Getenv, afero.NewOsFs())
	require.Error(t, err)
}

// --- fetchWithBrowser: timeout defaults ---

func TestFetchWithBrowser_ZeroTimeoutUsesDefault(t *testing.T) {
	// Zero timeout should default to 30s, but validation of URL happens first
	_, err := fetchWithBrowser(context.Background(), "", 0, nil, os.Getenv, afero.NewOsFs())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

// --- fetchWithBrowser: container detection affects Chrome flags ---

func TestFetchWithBrowser_ContainerDetectionViaChromeBin(t *testing.T) {
	origChromeBin := os.Getenv("CHROME_BIN")
	os.Setenv("CHROME_BIN", "/usr/bin/chromium")
	defer func() {
		if origChromeBin != "" {
			os.Setenv("CHROME_BIN", origChromeBin)
		} else {
			os.Unsetenv("CHROME_BIN")
		}
	}()

	// Will fail because chromedp can't actually launch Chrome, but tests the code path
	// that adds --no-sandbox and --disable-setuid-sandbox flags
	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, nil, os.Getenv, afero.NewOsFs())
	_ = err
}

func TestFetchWithBrowser_ContainerDetectionViaChromePath(t *testing.T) {
	origChromePath := os.Getenv("CHROME_PATH")
	os.Setenv("CHROME_PATH", "/usr/bin/google-chrome")
	defer func() {
		if origChromePath != "" {
			os.Setenv("CHROME_PATH", origChromePath)
		} else {
			os.Unsetenv("CHROME_PATH")
		}
	}()

	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, nil, os.Getenv, afero.NewOsFs())
	_ = err
}

// TestFetchWithBrowser_ContainerDetectedDisablesSandbox exercises the
// `IsRunningInContainer(fs) == true` branch of fetchWithBrowser by injecting an
// in-memory filesystem with /.dockerenv. On a non-container CI runner the real
// filesystem returns false, so without this test the no-sandbox flag path
// (the container-detected true branch) is never covered. The call still fails
// when chromedp tries to launch Chrome — we only need it to reach past the
// container-detection branch, mirroring the ViaChromeBin/ViaChromePath tests
// above.
func TestFetchWithBrowser_ContainerDetectedDisablesSandbox(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/.dockerenv", []byte(""), 0o644); err != nil {
		t.Fatalf("create /.dockerenv: %v", err)
	}
	require.True(t, system.IsRunningInContainer(fs), "test setup: fs must read as container")

	_, err := fetchWithBrowser(context.Background(), "https://www.dmm.co.jp/", 1, nil, os.Getenv, fs)
	_ = err
}
