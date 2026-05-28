package state

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/6Kmfi6HP/vpn-meridian/internal/csvparser"
)

// ServerState tracks a server's lifecycle in the incremental state.
type ServerState struct {
	ID                       string `json:"id"`
	Hostname                 string `json:"hostname,omitempty"`
	IP                       string `json:"ip,omitempty"`
	CountryShort             string `json:"countryshort"`
	CountryLong              string `json:"countrylong"`
	Ping                     string `json:"ping,omitempty"`
	Speed                    string `json:"speed,omitempty"`
	Status                   string `json:"status"`
	FirstSeen                int64  `json:"firstSeen"`
	LastSeen                 int64  `json:"lastSeen"`
	LastChanged              int64  `json:"lastChanged"`
	SeenCount                int    `json:"seenCount"`
	MissCount                int    `json:"missCount"`
	ConfigHash               string `json:"configHash"`
	ContentHash              string `json:"contentHash"`
	ConfigFilename           string `json:"configFilename,omitempty"`
	OpenVPNConfigDataBase64  string `json:"openvpn_configdata_base64,omitempty"`
}

// PublishedServer is a server included in the output data.
type PublishedServer struct {
	ID                       string `json:"id"`
	Hostname                 string `json:"hostname,omitempty"`
	IP                       string `json:"ip,omitempty"`
	CountryShort             string `json:"countryshort"`
	CountryLong              string `json:"countrylong"`
	Ping                     string `json:"ping,omitempty"`
	Speed                    string `json:"speed,omitempty"`
	Status                   string `json:"status"`
	FirstSeen                int64  `json:"firstSeen"`
	LastSeen                 int64  `json:"lastSeen"`
	LastChanged              int64  `json:"lastChanged"`
	SeenCount                int    `json:"seenCount"`
	MissCount                int    `json:"missCount"`
	ConfigHash               string `json:"configHash"`
	ContentHash              string `json:"contentHash"`
	ConfigFilename           string `json:"configFilename,omitempty"`
	OpenVPNConfigDataBase64  string `json:"openvpn_configdata_base64,omitempty"`
}

// ChangeSummary summarizes a server change.
type ChangeSummary struct {
	ID               string `json:"id"`
	Hostname         string `json:"hostname,omitempty"`
	IP               string `json:"ip,omitempty"`
	CountryShort     string `json:"countryshort"`
	CountryLong      string `json:"countrylong"`
	Status           string `json:"status"`
	ConfigFilename   string `json:"configFilename,omitempty"`
}

// Changes tracks all state transitions.
type Changes struct {
	Added          []ChangeSummary `json:"added"`
	Updated        []ChangeSummary `json:"updated"`
	Recovered      []ChangeSummary `json:"recovered"`
	Missing        []ChangeSummary `json:"missing"`
	Inactive       []ChangeSummary `json:"inactive"`
	Pruned         []ChangeSummary `json:"pruned"`
	UnchangedCount int             `json:"unchangedCount"`
}

// StateFile represents the full state file structure.
type StateFile struct {
	Version          int                    `json:"version"`
	GeneratedAt      int64                  `json:"generatedAt"`
	ActiveMissLimit  int                    `json:"activeMissLimit"`
	PruneMissLimit   int                    `json:"pruneMissLimit"`
	Servers          map[string]*ServerState `json:"servers"`
}

// CollectionStats holds scrape statistics.
type CollectionStats struct {
	TotalRequests         int
	SuccessfulRequests    int
	CollectedServerEntries int
	UniqueCurrentServers  int
}

// Statistics is the output statistics structure.
type Statistics struct {
	TotalRequests          int    `json:"totalRequests"`
	SuccessfulRequests     int    `json:"successfulRequests"`
	CollectedServerEntries int    `json:"collectedServerEntries"`
	UniqueCurrentServers   int    `json:"uniqueCurrentServers"`
	PublishedServers       int    `json:"publishedServers"`
	ActiveServers          int    `json:"activeServers"`
	MissingServers         int    `json:"missingServers"`
	InactiveServers        int    `json:"inactiveServers"`
	TotalStateServers      int    `json:"totalStateServers"`
	AddedServers           int    `json:"addedServers"`
	UpdatedServers         int    `json:"updatedServers"`
	RecoveredServers       int    `json:"recoveredServers"`
	MissingTransitions     int    `json:"missingTransitions"`
	InactiveTransitions    int    `json:"inactiveTransitions"`
	PrunedServers          int    `json:"prunedServers"`
	UnchangedServers       int    `json:"unchangedServers"`
	ActiveMissLimit        int    `json:"activeMissLimit"`
	PruneMissLimit         int    `json:"pruneMissLimit"`
}

// MergeResult is the full output of the merge process.
type MergeResult struct {
	GeneratedAt    int64
	GeneratedAtISO string
	Servers        []PublishedServer
	Countries      map[string]string
	State          *StateFile
	Changes        *Changes
	Statistics     *Statistics
}

// StateManager handles incremental state management.
type StateManager struct {
	statePath        string
	outputDir        string
	activeMissLimit  int
	pruneMissLimit   int
}

// New creates a StateManager.
func New(statePath, outputDir string, activeMissLimit, pruneMissLimit int) *StateManager {
	return &StateManager{
		statePath:       statePath,
		outputDir:       outputDir,
		activeMissLimit: activeMissLimit,
		pruneMissLimit:  pruneMissLimit,
	}
}

// hashSHA256 computes the SHA-256 hex digest of a string.
func hashSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// stableStringify produces deterministic JSON (keys sorted).
func stableStringify(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// buildServerIdentity creates a stable identity key for a server.
func buildServerIdentity(hostname, ip, countryShort, configDataBase64 string) string {
	if hostname != "" {
		return "host:" + strings.ToLower(hostname)
	}
	if ip != "" {
		return "ip:" + strings.ToLower(ip) + "|country:" + strings.ToLower(countryShort)
	}
	if configDataBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(configDataBase64)
		if err == nil {
			h := sha256.Sum256(decoded)
			return fmt.Sprintf("config:%x", h)
		}
	}
	return "unknown"
}

// slugify converts a string to a safe filename.
func slugify(s string) string {
	s = strings.ToLower(s)
	// Replace non-alphanumeric chars with hyphens
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	if len(result) > 60 {
		result = result[:60]
	}
	if result == "" {
		result = "server"
	}
	return result
}

// normalizeServer takes a raw API server and computes state fields.
func normalizeServer(s csvparser.Server, now int64) *ServerState {
	identity := buildServerIdentity(s.Hostname, s.IP, s.CountryShort, s.OpenVPNConfigDataBase64)
	idSlug := slugify(s.Hostname)
	if idSlug == "server" {
		idSlug = slugify(s.IP)
	}
	idHash := hashSHA256(identity)
	configHash := hashSHA256(s.OpenVPNConfigDataBase64)

	serverState := &ServerState{
		ID:                      idSlug + "-" + idHash[:10],
		Hostname:                s.Hostname,
		IP:                      s.IP,
		CountryShort:            s.CountryShort,
		CountryLong:             s.CountryLong,
		Ping:                    s.Ping,
		Speed:                   s.Speed,
		Status:                  "active",
		FirstSeen:               now,
		LastSeen:                now,
		LastChanged:             now,
		SeenCount:               1,
		MissCount:               0,
		ConfigHash:              configHash,
		ContentHash:             hashSHA256(stableStringify(s)),
		ConfigFilename:          idSlug + ".ovpn",
		OpenVPNConfigDataBase64: s.OpenVPNConfigDataBase64,
	}
	return serverState
}

// selectPreferredServer returns the one with higher speed.
func selectPreferredServer(a, b *ServerState) *ServerState {
	speedA, _ := strconv.Atoi(a.Speed)
	speedB, _ := strconv.Atoi(b.Speed)
	if speedB > speedA {
		return b
	}
	return a
}

// hydrateStateServerConfigs loads configs from a previous data.json snapshot.
func hydrateStateServerConfigs(state *StateFile, dataPath string) {
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		return
	}

	data, err := os.ReadFile(dataPath)
	if err != nil {
		return
	}

	var snapshot struct {
		Data struct {
			Servers []PublishedServer `json:"servers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return
	}

	configsByID := make(map[string]string)
	for _, s := range snapshot.Data.Servers {
		if s.OpenVPNConfigDataBase64 != "" {
			configsByID[s.ID] = s.OpenVPNConfigDataBase64
		}
	}

	for id, ss := range state.Servers {
		if ss.OpenVPNConfigDataBase64 == "" {
			if config, ok := configsByID[id]; ok {
				ss.OpenVPNConfigDataBase64 = config
			}
		}
	}
}

// LoadState reads the previous state from disk, or returns an empty state.
func (m *StateManager) LoadState() *StateFile {
	// Ensure directory exists
	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return m.emptyState()
	}

	data, err := os.ReadFile(m.statePath)
	if err != nil {
		return m.emptyState()
	}

	var state StateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return m.emptyState()
	}

	if state.Servers == nil {
		state.Servers = make(map[string]*ServerState)
	}

	// Hydrate configs from previous data.json
	dataPath := filepath.Join(filepath.Dir(m.statePath), "..", "json", "data.json")
	hydrateStateServerConfigs(&state, dataPath)

	return &state
}

func (m *StateManager) emptyState() *StateFile {
	return &StateFile{
		Version:         1,
		GeneratedAt:     0,
		ActiveMissLimit: m.activeMissLimit,
		PruneMissLimit:  m.pruneMissLimit,
		Servers:         make(map[string]*ServerState),
	}
}

// Merge processes the current scrape results against the previous state.
func (m *StateManager) Merge(
	currentServers []csvparser.Server,
	countries map[string]string,
	stats CollectionStats,
) *MergeResult {
	now := time.Now().UnixMilli()

	// Load previous state
	state := m.LoadState()
	if state.ActiveMissLimit == 0 {
		state.ActiveMissLimit = m.activeMissLimit
	}
	if state.PruneMissLimit == 0 {
		state.PruneMissLimit = m.pruneMissLimit
	}

	// Normalize current servers
	currentByID := make(map[string]*ServerState)
	for _, s := range currentServers {
		normalized := normalizeServer(s, now)
		existing, ok := currentByID[normalized.ID]
		if ok {
			currentByID[normalized.ID] = selectPreferredServer(existing, normalized)
		} else {
			currentByID[normalized.ID] = normalized
		}
	}

	changes := &Changes{
		Added:          make([]ChangeSummary, 0),
		Updated:        make([]ChangeSummary, 0),
		Recovered:      make([]ChangeSummary, 0),
		Missing:        make([]ChangeSummary, 0),
		Inactive:       make([]ChangeSummary, 0),
		Pruned:         make([]ChangeSummary, 0),
		UnchangedCount: 0,
	}

	// Track published servers
	publishedByID := make(map[string]*ServerState)

	// Process previous state servers (sorted by ID for determinism)
	ids := make([]string, 0, len(state.Servers))
	for id := range state.Servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	prunedIDs := make([]string, 0)
	for _, id := range ids {
		prev := state.Servers[id]

		if _, ok := currentByID[id]; ok {
			continue // will be handled in current server processing
		}

		// Server not found in current scrape
		prev.MissCount++

		if prev.MissCount >= m.activeMissLimit {
			prev.Status = "inactive"
			// Remove config from state
			config := prev.OpenVPNConfigDataBase64
			prev.OpenVPNConfigDataBase64 = ""
			changes.Inactive = append(changes.Inactive, ChangeSummary{
				ID:               prev.ID,
				Hostname:         prev.Hostname,
				IP:               prev.IP,
				CountryShort:     prev.CountryShort,
				CountryLong:      prev.CountryLong,
				Status:           prev.Status,
				ConfigFilename:   prev.ConfigFilename,
			})
			_ = config // previously removed config data
		} else {
			switch prev.Status {
			case "active":
				changes.Missing = append(changes.Missing, ChangeSummary{
					ID:               prev.ID,
					Hostname:         prev.Hostname,
					IP:               prev.IP,
					CountryShort:     prev.CountryShort,
					CountryLong:      prev.CountryLong,
					Status:           "missing",
					ConfigFilename:   prev.ConfigFilename,
				})
				prev.Status = "missing"
			case "missing":
				// Still missing, keep status
			}
		}

		// Prune check
		if prev.MissCount >= m.pruneMissLimit {
			changes.Pruned = append(changes.Pruned, ChangeSummary{
				ID:               prev.ID,
				Hostname:         prev.Hostname,
				IP:               prev.IP,
				CountryShort:     prev.CountryShort,
				CountryLong:      prev.CountryLong,
				Status:           prev.Status,
				ConfigFilename:   prev.ConfigFilename,
			})
			prunedIDs = append(prunedIDs, id)
			continue
		}

		// Keep missing servers in published output if they have config
		if (prev.Status == "missing" || prev.Status == "active") && prev.OpenVPNConfigDataBase64 != "" {
			publishedByID[id] = prev
		}
	}

	// Remove pruned servers
	for _, id := range prunedIDs {
		delete(state.Servers, id)
	}

	// Process current servers (sorted by ID for determinism)
	currentIDs := make([]string, 0, len(currentByID))
	for id := range currentByID {
		currentIDs = append(currentIDs, id)
	}
	sort.Strings(currentIDs)

	for _, id := range currentIDs {
		current := currentByID[id]
		prev, exists := state.Servers[id]

		if !exists {
			// New server
			state.Servers[id] = current
			publishedByID[id] = current
			changes.Added = append(changes.Added, ChangeSummary{
				ID:               current.ID,
				Hostname:         current.Hostname,
				IP:               current.IP,
				CountryShort:     current.CountryShort,
				CountryLong:      current.CountryLong,
				Status:           "active",
				ConfigFilename:   current.ConfigFilename,
			})
		} else {
			// Existing server - update
			if prev.ContentHash != current.ContentHash {
				prev.LastChanged = now
				changes.Updated = append(changes.Updated, ChangeSummary{
					ID:               prev.ID,
					Hostname:         prev.Hostname,
					IP:               prev.IP,
					CountryShort:     prev.CountryShort,
					CountryLong:      prev.CountryLong,
					Status:           "active",
					ConfigFilename:   prev.ConfigFilename,
				})
			} else {
				changes.UnchangedCount++
			}

			if prev.Status != "active" {
				changes.Recovered = append(changes.Recovered, ChangeSummary{
					ID:               prev.ID,
					Hostname:         prev.Hostname,
					IP:               prev.IP,
					CountryShort:     prev.CountryShort,
					CountryLong:      prev.CountryLong,
					Status:           "active",
					ConfigFilename:   prev.ConfigFilename,
				})
			}

			prev.Status = "active"
			prev.LastSeen = now
			prev.MissCount = 0
			prev.SeenCount++
			prev.Speed = current.Speed
			prev.Ping = current.Ping
			prev.ContentHash = current.ContentHash
			prev.ConfigHash = current.ConfigHash
			prev.OpenVPNConfigDataBase64 = current.OpenVPNConfigDataBase64

			publishedByID[id] = prev
		}
	}

	// Build published servers list
	publishedIDs := make([]string, 0, len(publishedByID))
	for id := range publishedByID {
		publishedIDs = append(publishedIDs, id)
	}
	sort.Strings(publishedIDs)

	published := make([]PublishedServer, 0, len(publishedByID))
	for _, id := range publishedIDs {
		s := publishedByID[id]
		published = append(published, PublishedServer{
			ID:                      s.ID,
			Hostname:                s.Hostname,
			IP:                      s.IP,
			CountryShort:            s.CountryShort,
			CountryLong:             s.CountryLong,
			Ping:                    s.Ping,
			Speed:                   s.Speed,
			Status:                  s.Status,
			FirstSeen:               s.FirstSeen,
			LastSeen:                s.LastSeen,
			LastChanged:             s.LastChanged,
			SeenCount:               s.SeenCount,
			MissCount:               s.MissCount,
			ConfigHash:              s.ConfigHash,
			ContentHash:             s.ContentHash,
			ConfigFilename:          s.ConfigFilename,
			OpenVPNConfigDataBase64: s.OpenVPNConfigDataBase64,
		})
	}

	// Compute statistics
	activeCount := 0
	missingCount := 0
	inactiveCount := 0
	for _, s := range state.Servers {
		switch s.Status {
		case "active":
			activeCount++
		case "missing":
			missingCount++
		case "inactive":
			inactiveCount++
		}
	}

	statsResult := &Statistics{
		TotalRequests:          stats.TotalRequests,
		SuccessfulRequests:     stats.SuccessfulRequests,
		CollectedServerEntries: stats.CollectedServerEntries,
		UniqueCurrentServers:   stats.UniqueCurrentServers,
		PublishedServers:       len(published),
		ActiveServers:          activeCount,
		MissingServers:         missingCount,
		InactiveServers:        inactiveCount,
		TotalStateServers:      len(state.Servers),
		AddedServers:           len(changes.Added),
		UpdatedServers:         len(changes.Updated),
		RecoveredServers:       len(changes.Recovered),
		MissingTransitions:     len(changes.Missing),
		InactiveTransitions:    len(changes.Inactive),
		PrunedServers:          len(changes.Pruned),
		UnchangedServers:       changes.UnchangedCount,
		ActiveMissLimit:        m.activeMissLimit,
		PruneMissLimit:         m.pruneMissLimit,
	}

	genTime := time.Now()

	return &MergeResult{
		GeneratedAt:    now,
		GeneratedAtISO: genTime.UTC().Format(time.RFC3339),
		Servers:        published,
		Countries:      countries,
		State:          state,
		Changes:        changes,
		Statistics:     statsResult,
	}
}
