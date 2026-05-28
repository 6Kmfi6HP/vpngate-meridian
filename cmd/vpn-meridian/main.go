package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/6Kmfi6HP/vpn-meridian/internal/config"
	"github.com/6Kmfi6HP/vpn-meridian/internal/maxmind"
	"github.com/6Kmfi6HP/vpn-meridian/internal/output"
	"github.com/6Kmfi6HP/vpn-meridian/internal/scraper"
	"github.com/6Kmfi6HP/vpn-meridian/internal/state"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "enrich" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		runEnrich()
		return
	}
	runScraper()
}

func runScraper() {
	cfg := config.Load()

	fmt.Println("Starting VPN Meridian")
	fmt.Printf("Total requests: %d\n", cfg.TotalRequests)
	fmt.Printf("Worker count: %d\n", runtime.NumCPU())

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	w := output.NewWriter(cfg.OutputDir)
	if err := w.EnsureDirectories(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directories: %v\n", err)
		os.Exit(1)
	}

	pool := scraper.NewPool(cfg)
	start := time.Now()

	currentServers, countries, err := pool.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running scraper: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(start)
	fmt.Printf("\nScraping completed in %s\n", elapsed.Round(time.Second))
	fmt.Printf("Collected %d server entries\n", len(currentServers))

	if len(currentServers) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no servers collected")
		os.Exit(1)
	}

	sm := state.New(cfg.StatePath, cfg.OutputDir, cfg.ActiveMissLimit, cfg.PruneMissLimit)
	result := sm.Merge(currentServers, countries, state.CollectionStats{
		TotalRequests:          cfg.TotalRequests,
		SuccessfulRequests:     len(currentServers),
		CollectedServerEntries: len(currentServers),
		UniqueCurrentServers:   len(currentServers),
	})

	fmt.Println("Saving output files...")

	if _, err := w.SaveVpnConfigs(result.Servers); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving VPN configs: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Saved VPN configs")

	if err := w.GenerateHomePage(result); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating homepage: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Generated homepage")

	if err := w.GenerateSitemap(); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating sitemap: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Generated sitemap.xml")

	if err := w.GenerateRobots(); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating robots.txt: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Generated robots.txt")

	if err := w.GenerateReadme(result); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating readme: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Generated readme")

	if err := w.SaveData(result); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving data: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Saved data.json")

	if err := w.SaveChanges(result); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving changes: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Saved changes.json")

	if err := w.SaveState(cfg.StatePath, result.State); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving state: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Saved state")

	stats := result.Statistics
	fmt.Println("\n=== Summary ===")
	fmt.Printf("Published servers: %d\n", stats.PublishedServers)
	fmt.Printf("Active servers: %d\n", stats.ActiveServers)
	fmt.Printf("Missing servers: %d\n", stats.MissingServers)
	fmt.Printf("Inactive servers: %d\n", stats.InactiveServers)
	fmt.Printf("Total state servers: %d\n", stats.TotalStateServers)
	fmt.Printf("Added: %d | Updated: %d | Recovered: %d\n",
		stats.AddedServers, stats.UpdatedServers, stats.RecoveredServers)
	fmt.Printf("Missing transitions: %d | Inactive transitions: %d | Pruned: %d\n",
		stats.MissingTransitions, stats.InactiveTransitions, stats.PrunedServers)
	fmt.Printf("Unchanged: %d\n", stats.UnchangedServers)
}

func runEnrich() {
	fs := flag.NewFlagSet("enrich", flag.ExitOnError)
	inputPath := fs.String("input", getEnvStr("VPNGATE_INPUT_FILE", "public/json/data.json"), "Input data.json path")
	outputPath := fs.String("output", getEnvStr("MAXMIND_OUTPUT_FILE", "public/json/data.maxmind.json"), "Output enriched JSON path")
	mihomoOutput := fs.String("mihomo-output", getEnvStr("MIHOMO_OUTPUT_FILE", "public/mihomo_openvpn.yaml"), "Output mihomo YAML path")
	maxmindDir := fs.String("maxmind-dir", getEnvStr("MAXMIND_DB_DIR", "maxmind"), "MaxMind database directory")
	_ = mihomoOutput
	_ = fs.Parse(os.Args[1:])

	fmt.Println("Running MaxMind enrichment...")
	fmt.Printf("Input: %s\n", *inputPath)
	fmt.Printf("Output: %s\n", *outputPath)
	fmt.Printf("MaxMind dir: %s\n", *maxmindDir)

	if err := maxmind.Enrich(*inputPath, *outputPath, *maxmindDir); err != nil {
		fmt.Fprintf(os.Stderr, "MaxMind enrichment failed: %v\n", err)
		os.Exit(1)
	}

	if err := maxmind.BuildMihomoConfig(*inputPath, *mihomoOutput); err != nil {
		fmt.Fprintf(os.Stderr, "Mihomo config generation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Enrichment completed successfully")
	fmt.Printf("  Enriched data: %s\n", *outputPath)
	fmt.Printf("  Mihomo config: %s\n", *mihomoOutput)
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
