package scraper

import (
	"context"
	"fmt"
	"maps"
	"runtime"
	"sync"

	"github.com/6Kmfi6HP/vpn-meridian/internal/config"
	"github.com/6Kmfi6HP/vpn-meridian/internal/csvparser"
)

// WorkerResult holds results from a single worker.
type WorkerResult struct {
	Servers   []csvparser.Server
	Countries map[string]string
}

// Pool manages concurrent scraper workers.
type Pool struct {
	cfg         *config.Config
	workerCount int
}

// NewPool creates a worker pool with configured concurrency.
func NewPool(cfg *config.Config) *Pool {
	workers := cfg.WorkerCount
	if workers <= 0 {
		workers = 1
	}
	// Cap at CPU count - 1 like the JS version
	numCPU := runtime.NumCPU()
	if numCPU > 1 && workers > numCPU-1 {
		workers = numCPU - 1
	}
	// But never go below 1
	if workers < 1 {
		workers = 1
	}
	// If total requests is small, don't use more workers than needed
	if cfg.TotalRequests < workers {
		workers = cfg.TotalRequests
	}
	return &Pool{cfg: cfg, workerCount: workers}
}

// Run distributes requests across workers and returns combined results.
func (p *Pool) Run(ctx context.Context) ([]csvparser.Server, map[string]string, error) {
	perWorker := distributeRequests(p.cfg.TotalRequests, p.workerCount)
	results := make(chan WorkerResult, p.workerCount)

	var wg sync.WaitGroup
	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func(workerID, count int) {
			defer wg.Done()
			result := p.runWorker(ctx, workerID, count)
			results <- result
		}(i, perWorker[i])
	}

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results
	allServers := make([]csvparser.Server, 0, p.cfg.TotalRequests)
	allCountries := make(map[string]string)

	for r := range results {
		allServers = append(allServers, r.Servers...)
		maps.Copy(allCountries, r.Countries)
	}

	// Deduplicate across all results
	unique := deduplicateServers(allServers)
	return unique, allCountries, nil
}

func (p *Pool) runWorker(ctx context.Context, workerID, count int) WorkerResult {
	s := New(p.cfg)
	result := WorkerResult{
		Servers:   make([]csvparser.Server, 0, count),
		Countries: make(map[string]string),
	}

	for i := range count {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		data, err := s.Fetch(ctx)
		if err != nil {
			if !p.cfg.NoProgress && !p.cfg.IsCI {
				fmt.Printf("\rWorker %d: request %d/%d failed: %v", workerID, i+1, count, err)
			}
			continue
		}

		result.Servers = append(result.Servers, data.Servers...)
		maps.Copy(result.Countries, data.Countries)

		if !p.cfg.NoProgress && !p.cfg.IsCI {
			fmt.Printf("\rWorker %d: %d/%d requests completed", workerID, i+1, count)
		}
	}

	return result
}

func distributeRequests(total, workers int) []int {
	if workers <= 0 {
		return []int{total}
	}
	perWorker := make([]int, workers)
	base := total / workers
	remainder := total % workers
	for i := 0; i < workers; i++ {
		perWorker[i] = base
		if i < remainder {
			perWorker[i]++
		}
	}
	return perWorker
}

func deduplicateServers(servers []csvparser.Server) []csvparser.Server {
	seen := make(map[string]struct{}, len(servers))
	unique := make([]csvparser.Server, 0, len(servers))
	for _, s := range servers {
		key := s.Hostname
		if key == "" {
			key = s.IP
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			unique = append(unique, s)
		}
	}
	return unique
}
