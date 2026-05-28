package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ProxyResult struct {
	Name      string `json:"name"`
	Alive     bool   `json:"alive"`
	LatencyMs int    `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
}

type TestOutput struct {
	GeneratedAt string        `json:"generatedAt"`
	Stats       TestStats     `json:"statistics"`
	Results     []ProxyResult `json:"results"`
}

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

type DataServer struct {
	ID           string `json:"id"`
	Hostname     string `json:"hostname"`
	IP           string `json:"ip"`
	CountryShort string `json:"countryshort"`
	CountryLong  string `json:"countrylong"`
	Ping         string `json:"ping"`
	Speed        string `json:"speed"`
	Status       string `json:"status"`
	ConfigPath   string `json:"configPath,omitempty"`
}

type TestDataEntry struct {
	Alive     bool   `json:"alive"`
	TestedAt  string `json:"testedAt"`
	LatencyMs *int   `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
}

type DataFile struct {
	GeneratedAt    int64              `json:"generatedAt"`
	GeneratedAtISO string             `json:"generatedAtIso"`
	Data           DataFileContent    `json:"data"`
	Statistics     map[string]any     `json:"statistics,omitempty"`
}

type DataFileContent struct {
	Servers   []DataServer        `json:"servers"`
	Countries map[string]string   `json:"countries"`
}

type TestedDataFile struct {
	GeneratedAt    int64              `json:"generatedAt"`
	GeneratedAtISO string             `json:"generatedAtIso"`
	Data           DataFileContent    `json:"data"`
	Statistics     map[string]any     `json:"statistics,omitempty"`
	Test           *TestDataMeta      `json:"test,omitempty"`
}

type TestDataMeta struct {
	GeneratedAt string `json:"generatedAt"`
	Statistics  any    `json:"statistics"`
}

type MihomoConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

func main() {
	inputFile := flag.String("mihomo-input", getEnvStr("MIHOMO_INPUT_FILE", "public/mihomo_openvpn.yaml"), "Input mihomo YAML file")
	dataInput := flag.String("data-input", getEnvStr("TEST_DATA_INPUT_FILE", "public/json/data.json"), "Input data.json file")
	testedData := flag.String("tested-data", getEnvStr("TESTED_DATA_FILE", "public/json/data.tested.json"), "Output tested data file")
	aliveMihomo := flag.String("alive-mihomo", getEnvStr("ALIVE_MIHOMO_FILE", "public/mihomo_tested_openvpn.yaml"), "Output alive mihomo YAML")
	resultsJSON := flag.String("results-json", getEnvStr("RESULTS_JSON_FILE", "public/json/test_results.json"), "Output test results JSON")
	maxAlive := flag.Int("max-alive", getEnvInt("TEST_MAX_ALIVE", 0), "Max alive proxies (0 = no limit)")
	timeout := flag.Int("timeout", getEnvInt("TEST_TIMEOUT", 10), "Per-proxy timeout (seconds)")
	workers := flag.Int("workers", getEnvInt("TEST_WORKERS", 20), "Concurrent workers")
	flag.Parse()

	testerDir := filepath.Join("scripts", "tester")
	testerBinary := filepath.Join(testerDir, "tester")

	if !fileExists(testerBinary) {
		fmt.Println("Building Go tester binary...")
		cmd := exec.Command("go", "build", "-tags", "with_gvisor", "-o", testerBinary, ".")
		cmd.Dir = testerDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to build tester: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Tester built successfully")
	}

	fmt.Println("Running OpenVPN server tests...")
	fmt.Printf("Input: %s\n", *inputFile)
	fmt.Printf("Timeout: %ds, Workers: %d\n", *timeout, *workers)

	testOutput, err := runTester(testerBinary, *inputFile, *timeout, *workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Tester failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Test results: %d alive / %d total (%.1f%%)\n",
		testOutput.Stats.Alive, testOutput.Stats.Total, testOutput.Stats.AliveRate)

	data, err := loadDataFile(*dataInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load data: %v\n", err)
		os.Exit(1)
	}

	aliveNames := make(map[string]int)
	for _, r := range testOutput.Results {
		if r.Alive {
			aliveNames[r.Name] = r.LatencyMs
		}
	}

	proxyToServer := buildProxyToServerMap(data.Data.Servers, aliveNames)

	testedDataOut := generateTestedData(data, testOutput, proxyToServer)
	if err := atomicWriteJSON(*testedData, testedDataOut); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write tested data: %v\n", err)
		os.Exit(1)
	}

	if err := atomicWriteJSON(*resultsJSON, testOutput); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write results: %v\n", err)
		os.Exit(1)
	}

	if err := generateAliveYAML(*inputFile, *aliveMihomo, aliveNames, *maxAlive); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write alive YAML: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Test completed successfully")
	fmt.Printf("  Tested data: %s\n", *testedData)
	fmt.Printf("  Results: %s\n", *resultsJSON)
	fmt.Printf("  Alive mihomo: %s\n", *aliveMihomo)
}

func runTester(binary, inputFile string, timeout, workers int) (*TestOutput, error) {
	cmd := exec.Command(binary,
		"--input", inputFile,
		"--timeout", strconv.Itoa(timeout),
		"--workers", strconv.Itoa(workers),
	)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tester execution failed: %w", err)
	}

	var result TestOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse tester output: %w", err)
	}
	return &result, nil
}

func loadDataFile(path string) (*DataFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f DataFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func extractHostname(proxyName string) string {
	parts := strings.Split(proxyName, " ")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return proxyName
}

func buildProxyToServerMap(servers []DataServer, aliveNames map[string]int) map[string]int {
	hostnameToIdx := make(map[string]int)
	for i, s := range servers {
		hostname := s.Hostname
		if hostname == "" {
			hostname = s.IP
		}
		if hostname != "" {
			hostnameToIdx[hostname] = i
		}
	}

	result := make(map[string]int)
	for name := range aliveNames {
		hostname := extractHostname(name)
		if idx, ok := hostnameToIdx[hostname]; ok {
			result[name] = idx
		}
	}
	return result
}

func generateTestedData(data *DataFile, testOutput *TestOutput, proxyToServer map[string]int) *TestedDataFile {
	now := time.Now().UTC().Format(time.RFC3339)

	serverTests := make(map[int]*TestDataEntry)
	for _, r := range testOutput.Results {
		if idx, ok := proxyToServer[r.Name]; ok {
			entry := &TestDataEntry{
				Alive:    r.Alive,
				TestedAt: now,
			}
			if r.Alive {
				entry.LatencyMs = &r.LatencyMs
			} else if r.Error != "" {
				entry.Error = r.Error
			}
			serverTests[idx] = entry
		}
	}

	servers := make([]DataServer, len(data.Data.Servers))
	copy(servers, data.Data.Servers)

	testedServers := make([]map[string]any, len(servers))
	for i, s := range servers {
		entry := make(map[string]any)
		entry["id"] = s.ID
		entry["hostname"] = s.Hostname
		entry["ip"] = s.IP
		entry["countryshort"] = s.CountryShort
		entry["countrylong"] = s.CountryLong
		entry["ping"] = s.Ping
		entry["speed"] = s.Speed
		entry["status"] = s.Status
		if test, ok := serverTests[i]; ok {
			entry["test"] = test
		}
		testedServers[i] = entry
	}

	return &TestedDataFile{
		GeneratedAt:    data.GeneratedAt,
		GeneratedAtISO: data.GeneratedAtISO,
		Data: DataFileContent{
			Servers:   servers,
			Countries: data.Data.Countries,
		},
		Statistics: data.Statistics,
		Test: &TestDataMeta{
			GeneratedAt: now,
			Statistics:  testOutput.Stats,
		},
	}
}

func generateAliveYAML(inputPath, outputPath string, aliveNames map[string]int, maxAlive int) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}

	var cfg MihomoConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	// Filter alive proxies and sort by latency
	type proxyWithLatency struct {
		proxy   map[string]any
		latency int
	}

	var alive []proxyWithLatency
	for _, p := range cfg.Proxies {
		name, _ := p["name"].(string)
		if latency, ok := aliveNames[name]; ok {
			alive = append(alive, proxyWithLatency{proxy: p, latency: latency})
		}
	}

	sort.Slice(alive, func(i, j int) bool {
		return alive[i].latency < alive[j].latency
	})

	if maxAlive > 0 && len(alive) > maxAlive {
		alive = alive[:maxAlive]
	}

	var sb strings.Builder
	sb.WriteString("proxies:\n")

	for _, item := range alive {
		sb.WriteString(fmt.Sprintf("  # latency: %dms\n", item.latency))
		first := true
		for key, value := range item.proxy {
			if first {
				sb.WriteString("  -")
				first = false
			} else {
				sb.WriteString("   ")
			}
			sb.WriteString(fmt.Sprintf(" %s: ", key))
			sb.WriteString(formatYAMLValue(value))
			sb.WriteString("\n")
		}
	}

	return atomicWriteText(outputPath, sb.String())
}

func formatYAMLValue(value any) string {
	switch v := value.(type) {
	case string:
		if strings.Contains(v, "\n") {
			lines := strings.Split(v, "\n")
			result := "|\n"
			for _, line := range lines {
				result += "      " + line + "\n"
			}
			return strings.TrimRight(result, "\n")
		}
		return fmt.Sprintf("%q", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return "null"
	default:
		data, _ := json.Marshal(v)
		return fmt.Sprintf("%q", string(data))
	}
}

func atomicWriteJSON(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, jsonData, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func atomicWriteText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func getEnvStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
