package httpclient

import (
	"github.com/javinizer/javinizer-go/internal/config"
)

// StandardHTMLHeaders returns the default HTTP headers for browser-like HTML requests.
func StandardHTMLHeaders() map[string]string {
	return map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9",
		"Accept-Encoding":           "gzip, deflate",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
	}
}

// JSONAPIHeaders returns HTTP headers for JSON API requests.
func JSONAPIHeaders() map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "en-US,en;q=0.9",
		"Accept-Encoding": "gzip, deflate",
		"Connection":      "keep-alive",
	}
}

// JapaneseLanguageHeaders returns browser-like HTML headers that prefer
// Japanese content while retaining an English fallback.
func JapaneseLanguageHeaders() map[string]string {
	return map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language":           "ja,en-US;q=0.8,en;q=0.6",
		"Accept-Encoding":           "gzip, deflate",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
	}
}

// RefererHeader returns a header map that sets the Referer to the given URL.
func RefererHeader(url string) map[string]string {
	return map[string]string{
		"Referer": url,
	}
}

// DMMHeaders returns HTTP headers for DMM requests, including age-check and locale cookies.
func DMMHeaders() map[string]string {
	return map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9,ja;q=0.8",
		"Accept-Encoding":           "gzip, deflate",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
		"Cookie":                    "age_check_done=1; cklg=ja",
	}
}

// R18DevHeaders returns HTTP headers for requests to the R18 dev API.
func R18DevHeaders() map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "en-US,en;q=0.9",
		"Accept-Encoding": "gzip, deflate",
		"Connection":      "keep-alive",
	}
}

// UserAgentHeader returns a header map with the User-Agent resolved from the given value.
func UserAgentHeader(ua string) map[string]string {
	resolved := config.ResolveScraperUserAgent(ua)
	return map[string]string{
		"User-Agent": resolved,
	}
}

// CombineHeaders merges the given header presets into a single header map.
func CombineHeaders(presets ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, preset := range presets {
		for k, v := range preset {
			result[k] = v
		}
	}
	return result
}
