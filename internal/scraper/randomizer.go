package scraper

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
}

func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func RandomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func RandomCookie() string {
	return fmt.Sprintf("vid=%s; sessionId=%s; visited=true", RandomString(12), RandomString(16))
}

func BuildHeaders() http.Header {
	h := http.Header{}
	h.Set("User-Agent", RandomUserAgent())
	h.Set("Cookie", RandomCookie())
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Cache-Control", "no-cache")
	return h
}

func RandomQueryString() string {
	return fmt.Sprintf("t=%d&nonce=%s&r=%f",
		// Seed timestamp to second precision like the JS version
		// We use unix timestamp directly
		0, // placeholder
		RandomString(10),
		rand.Float64(),
	)
}

// GenerateQueryParams generates random query parameters for the API request.
func GenerateQueryParams() map[string]string {
	return map[string]string{
		"t":     fmt.Sprintf("%d", 0), // will be replaced with actual timestamp
		"nonce": RandomString(10),
		"r":     fmt.Sprintf("%f", rand.Float64()),
	}
}

// FormatQueryParams formats query parameters as a URL query string.
func FormatQueryParams(params map[string]string) string {
	parts := make([]string, 0, len(params))
	for k, v := range params {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "&")
}
