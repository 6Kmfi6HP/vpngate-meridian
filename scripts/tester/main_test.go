package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseTestURLsSplitsCommaAndWhitespace(t *testing.T) {
	got := parseTestURLs(" http://one.test/generate_204,https://two.test/check\nhttp://three.test/ ")
	want := []string{
		"http://one.test/generate_204",
		"https://two.test/check",
		"http://three.test/",
	}

	if len(got) != len(want) {
		t.Fatalf("parseTestURLs length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseTestURLs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProbeHTTPTriesNextURLAfterFailure(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer bad.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer good.Close()

	transport := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{
		Timeout:   time.Second,
		Transport: transport,
	}
	defer client.CloseIdleConnections()

	result := probeHTTP(context.Background(), client, []string{bad.URL, good.URL}, time.Second)
	if !result.alive {
		t.Fatalf("probeHTTP alive = false, error = %q", result.err)
	}
	if result.url != good.URL {
		t.Fatalf("probeHTTP url = %q, want %q", result.url, good.URL)
	}
}
