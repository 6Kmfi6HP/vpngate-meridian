package output

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/6Kmfi6HP/vpn-meridian/internal/state"
)

//go:embed template.html
var templateFS embed.FS

func escapeHTML(s string) string {
	return html.EscapeString(s)
}

func formatSpeedMbps(speed string) string {
	n, err := strconv.ParseFloat(speed, 64)
	if err != nil {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", n/10000000)
}

func (w *Writer) GenerateHomePage(result *state.MergeResult) error {
	templateData, err := templateFS.ReadFile("template.html")
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}

	servers := make([]state.PublishedServer, len(result.Servers))
	copy(servers, result.Servers)
	sort.Slice(servers, func(i, j int) bool {
		speedI, _ := strconv.Atoi(servers[i].Speed)
		speedJ, _ := strconv.Atoi(servers[j].Speed)
		return speedI > speedJ
	})
	if len(servers) > 50 {
		servers = servers[:50]
	}

	type topServer struct {
		ID           string `json:"id"`
		Hostname     string `json:"hostname"`
		IP           string `json:"ip"`
		Ping         string `json:"ping"`
		Speed        string `json:"speed"`
		CountryLong  string `json:"countrylong"`
		CountryShort string `json:"countryshort"`
		Status       string `json:"status"`
	}
	topList := make([]topServer, 0, len(servers))
	for _, s := range servers {
		topList = append(topList, topServer{
			ID:           s.ID,
			Hostname:     s.Hostname,
			IP:           s.IP,
			Ping:         s.Ping,
			Speed:        s.Speed,
			CountryLong:  s.CountryLong,
			CountryShort: s.CountryShort,
			Status:       s.Status,
		})
	}
	topJSON, _ := json.Marshal(topList)
	topJSONStr := strings.ReplaceAll(string(topJSON), "</", `<\/`)

	countriesJSON, _ := json.Marshal(result.Countries)
	countriesJSONStr := strings.ReplaceAll(string(countriesJSON), "</", `<\/`)

	stats := result.Statistics
	countriesCount := len(result.Countries)

	var topSignals strings.Builder
	for i, s := range servers {
		if i >= 8 {
			break
		}
		code := s.CountryShort
		if code == "" {
			code = "N/A"
		}
		host := s.Hostname
		if host == "" {
			host = s.IP
		}
		if host == "" {
			host = s.ID
		}
		topSignals.WriteString(fmt.Sprintf(
			`                    <div class="signal-row"><span>%s / %s</span><strong>%s Mbps</strong></div>`,
			escapeHTML(code), escapeHTML(host), formatSpeedMbps(s.Speed),
		))
		topSignals.WriteString("\n")
	}

	publishedDisplay := stats.PublishedServers
	if publishedDisplay == 0 {
		publishedDisplay = len(result.Servers)
	}

	type initialState struct {
		Servers   json.RawMessage `json:"servers"`
		Countries json.RawMessage `json:"countries"`
		GeneratedAt string        `json:"generatedAt"`
		Statistics  struct {
			PublishedServers int `json:"publishedServers"`
			ActiveServers    int `json:"activeServers"`
			TotalCountries   int `json:"totalCountries"`
			TotalRequests    int `json:"totalRequests"`
		} `json:"statistics"`
	}
	initState := initialState{
		Servers:     json.RawMessage(topJSONStr),
		Countries:   json.RawMessage(countriesJSONStr),
		GeneratedAt: result.GeneratedAtISO,
	}
	initState.Statistics.PublishedServers = publishedDisplay
	initState.Statistics.ActiveServers = stats.ActiveServers
	initState.Statistics.TotalCountries = countriesCount
	initState.Statistics.TotalRequests = stats.TotalRequests
	initStateJSON, _ := json.Marshal(initState)
	initStateStr := "var __initialState = " + strings.ReplaceAll(string(initStateJSON), "</", `<\/`) + ";"

	content := string(templateData)
	content = strings.ReplaceAll(content, "{{INITIAL_STATE_JSON}}", initStateStr)
	content = strings.ReplaceAll(content, "{{UPDATED_AT}}", escapeHTML(result.GeneratedAtISO))
	content = strings.ReplaceAll(content, "{{PUBLISHED_SERVERS}}", escapeHTML(strconv.Itoa(publishedDisplay)))
	content = strings.ReplaceAll(content, "{{ACTIVE_SERVERS}}", escapeHTML(strconv.Itoa(stats.ActiveServers)))
	content = strings.ReplaceAll(content, "{{TOTAL_COUNTRIES}}", escapeHTML(strconv.Itoa(countriesCount)))
	content = strings.ReplaceAll(content, "{{TOTAL_REQUESTS}}", escapeHTML(strconv.Itoa(stats.TotalRequests)))
	content = strings.ReplaceAll(content, "{{GENERATED_AT_ISO}}", escapeHTML(result.GeneratedAtISO))
	content = strings.ReplaceAll(content, "{{TOP_SERVERS_SIGNALS}}", topSignals.String())
	content = strings.ReplaceAll(content, "{{SERVER_COUNT}}", strconv.Itoa(publishedDisplay))

	return os.WriteFile(
		filepath.Join(w.OutputDir, "index.html"),
		[]byte(content),
		0644,
	)
}
