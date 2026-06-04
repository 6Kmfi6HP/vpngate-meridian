package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
	"gopkg.in/yaml.v3"
)

// MihomoConfig matches the mihomo YAML proxy list.
type MihomoConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

// ProxyResult for a single proxy test.
type ProxyResult struct {
	Name      string `json:"name"`
	Alive     bool   `json:"alive"`
	LatencyMs int    `json:"latencyMs,omitempty"`
	TestURL   string `json:"testUrl,omitempty"`
	Error     string `json:"error,omitempty"`
}

type probeResult struct {
	alive   bool
	latency time.Duration
	url     string
	err     string
}

const defaultTestURLs = "http://gstatic.com/generate_204"

// TestStats aggregates all test results.
type TestStats struct {
	Total      int     `json:"total"`
	Tested     int     `json:"tested"`
	Alive      int     `json:"alive"`
	Dead       int     `json:"dead"`
	AliveRate  float64 `json:"aliveRate"`
	AvgLatency float64 `json:"avgLatency,omitempty"`
	P50Latency int     `json:"p50Latency,omitempty"`
	P90Latency int     `json:"p90Latency,omitempty"`
}

// TestOutput is the full JSON output written to stdout.
type TestOutput struct {
	GeneratedAt string        `json:"generatedAt"`
	Stats       TestStats     `json:"statistics"`
	Results     []ProxyResult `json:"results"`
}

func main() {
	inputFile := flag.String("input", "", "Input mihomo YAML file (default: stdin)")
	workers := flag.Int("workers", runtime.NumCPU()*2, "Concurrent workers")
	timeoutSec := flag.Int("timeout", 10, "Per-proxy test timeout (seconds)")
	testURL := flag.String("test-url", defaultTestURLs, "Comma or whitespace separated test URLs for latency measurement")
	attempts := flag.Int("attempts", 1, "Passes over the test URL list per proxy")
	shuffle := flag.Bool("shuffle", true, "Shuffle test order while preserving output order")
	flag.Parse()

	// Read YAML from file or stdin.
	var data []byte
	var err error
	if *inputFile != "" {
		data, err = os.ReadFile(*inputFile)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		log.Fatalf("Failed to read input: %v", err)
	}

	var cfg MihomoConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse YAML: %v", err)
	}

	proxies := cfg.Proxies
	if len(proxies) == 0 {
		log.Fatal("No proxies found in YAML")
	}

	testURLs := parseTestURLs(*testURL)
	if len(testURLs) == 0 {
		log.Fatal("No test URLs configured")
	}
	if *attempts < 1 {
		*attempts = 1
	}

	log.Printf("Testing %d proxies with %d workers, %d URL(s), %d attempt(s) ...", len(proxies), *workers, len(testURLs), *attempts)

	timeout := time.Duration(*timeoutSec) * time.Second
	results := make([]ProxyResult, len(proxies))
	progress := make(chan struct{}, len(proxies))

	var wg sync.WaitGroup
	sem := make(chan struct{}, *workers)
	order := make([]int, len(proxies))
	for i := range order {
		order[i] = i
	}
	if *shuffle {
		rand.Shuffle(len(order), func(i, j int) {
			order[i], order[j] = order[j], order[i]
		})
	}

	for _, i := range order {
		p := proxies[i]
		wg.Add(1)
		go func(idx int, mapping map[string]any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := testSingle(context.Background(), mapping, timeout, testURLs, *attempts)
			results[idx] = r
			progress <- struct{}{}
		}(i, p)
	}

	// Progress logger goroutine.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		doneCount := 0
		for {
			select {
			case <-progress:
				doneCount++
			case <-ticker.C:
				log.Printf("Progress: %d/%d tested", doneCount, len(proxies))
			case <-done:
				return
			}
		}
	}()

	wg.Wait()
	close(done)

	// Log error summary to stderr for CI diagnostics.
	errCounts := map[string]int{}
	for _, r := range results {
		if r.Error != "" {
			errCounts[r.Error]++
		}
	}
	if len(errCounts) > 0 {
		log.Printf("Error breakdown (%d unique):", len(errCounts))
		for err, count := range errCounts {
			log.Printf("  [%d] %s", count, err)
		}
	}

	// Build output.
	output := TestOutput{
		GeneratedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Stats:       computeStats(results),
		Results:     results,
	}

	// Write JSON to stdout.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		log.Fatalf("Failed to encode output: %v", err)
	}
}

func parseTestURLs(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	urls := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		u := strings.TrimSpace(part)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	return urls
}

func testSingle(ctx context.Context, mapping map[string]any, timeout time.Duration, testURLs []string, attempts int) ProxyResult {
	name, _ := mapping["name"].(string)
	if name == "" {
		name = "unknown"
	}

	proxy, err := adapter.ParseProxy(mapping)
	if err != nil {
		return ProxyResult{Name: name, Alive: false, Error: fmt.Sprintf("parse error: %v", err)}
	}
	defer func() {
		// Close in a goroutine with timeout to prevent blocking.
		done := make(chan struct{})
		go func() {
			proxy.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			var u16Port uint16
			if p, err := strconv.ParseUint(portStr, 10, 16); err == nil {
				u16Port = uint16(p)
			}
			return proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: u16Port,
			})
		},
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	defer client.CloseIdleConnections()

	var last probeResult
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		last = probeHTTP(ctx, client, testURLs, timeout)
		if last.alive {
			return ProxyResult{
				Name:      name,
				Alive:     true,
				LatencyMs: int(last.latency.Milliseconds()),
				TestURL:   last.url,
			}
		}
	}

	return ProxyResult{
		Name:  name,
		Alive: false,
		Error: last.err,
	}
}

func probeHTTP(ctx context.Context, client *http.Client, testURLs []string, timeout time.Duration) probeResult {
	if len(testURLs) == 0 {
		return probeResult{err: "no test URLs configured"}
	}

	errors := make([]string, 0, len(testURLs))
	for _, testURL := range testURLs {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, testURL, nil)
		if err != nil {
			cancel()
			errors = append(errors, fmt.Sprintf("%s: request error: %v", testURL, err))
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")
		req.Header.Set("Connection", "close")

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)
		if err != nil {
			cancel()
			errors = append(errors, fmt.Sprintf("%s: %v", testURL, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return probeResult{
				alive:   true,
				latency: latency,
				url:     testURL,
			}
		}
		errors = append(errors, fmt.Sprintf("%s: HTTP %d", testURL, resp.StatusCode))
	}

	return probeResult{
		err: strings.Join(errors, "; "),
	}
}

func computeStats(results []ProxyResult) TestStats {
	total := len(results)
	var alive int
	var delays []int
	for _, r := range results {
		if r.Alive {
			alive++
			delays = append(delays, r.LatencyMs)
		}
	}

	stats := TestStats{
		Total:  total,
		Tested: total,
		Alive:  alive,
		Dead:   total - alive,
	}
	if total > 0 {
		round := func(f float64) float64 {
			return float64(int(f*10)) / 10
		}
		stats.AliveRate = round(float64(alive) / float64(total) * 100)
	}
	if len(delays) > 0 {
		sort.Ints(delays)
		sum := 0
		for _, d := range delays {
			sum += d
		}
		round := func(f float64) float64 {
			return float64(int(f*10)) / 10
		}
		stats.AvgLatency = round(float64(sum) / float64(len(delays)))
		stats.P50Latency = delays[len(delays)/2]
		p90Idx := len(delays) * 9 / 10
		if p90Idx >= len(delays) {
			p90Idx = len(delays) - 1
		}
		stats.P90Latency = delays[p90Idx]
	}
	return stats
}
