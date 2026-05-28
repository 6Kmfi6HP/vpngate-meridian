package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
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
	Error     string `json:"error,omitempty"`
}

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
	testURL := flag.String("test-url", "http://www.gstatic.com/generate_204", "Test URL for latency measurement")
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

	log.Printf("Testing %d proxies with %d workers ...", len(proxies), *workers)

	timeout := time.Duration(*timeoutSec) * time.Second
	results := make([]ProxyResult, len(proxies))
	progress := make(chan struct{}, len(proxies))

	var wg sync.WaitGroup
	sem := make(chan struct{}, *workers)

	for i, p := range proxies {
		wg.Add(1)
		go func(idx int, mapping map[string]any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := testSingle(context.Background(), mapping, timeout, *testURL)
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

func testSingle(ctx context.Context, mapping map[string]any, timeout time.Duration, testURL string) ProxyResult {
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

	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return ProxyResult{Name: name, Alive: false, Error: fmt.Sprintf("request error: %v", err)}
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return ProxyResult{Name: name, Alive: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ProxyResult{
			Name:      name,
			Alive:     true,
			LatencyMs: int(time.Since(start).Milliseconds()),
		}
	}
	return ProxyResult{
		Name:  name,
		Alive: false,
		Error: fmt.Sprintf("HTTP %d", resp.StatusCode),
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
