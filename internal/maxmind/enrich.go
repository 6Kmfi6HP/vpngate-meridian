package maxmind

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

type CountryRecord struct {
	IsoCode       string            `json:"iso_code,omitempty"`
	Name          string            `json:"name,omitempty"`
	Names         map[string]string `json:"names,omitempty"`
	IsInEU        bool              `json:"is_in_european_union,omitempty"`
}

type ContinentRecord struct {
	Code  string            `json:"code,omitempty"`
	Name  string            `json:"name,omitempty"`
	Names map[string]string `json:"names,omitempty"`
}

type CityRecord struct {
	Name      string            `json:"name,omitempty"`
	Names     map[string]string `json:"names,omitempty"`
	GeoNameID uint              `json:"geoname_id,omitempty"`
}

type SubdivisionRecord struct {
	IsoCode   string            `json:"iso_code,omitempty"`
	Name      string            `json:"name,omitempty"`
	Names     map[string]string `json:"names,omitempty"`
	GeoNameID uint              `json:"geoname_id,omitempty"`
}

type LocationRecord struct {
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
	TimeZone  string  `json:"time_zone,omitempty"`
}

type ASNRecord struct {
	AutonomousSystemNumber       uint   `json:"autonomous_system_number,omitempty"`
	AutonomousSystemOrganization string `json:"autonomous_system_organization,omitempty"`
}

type MaxMindRecord struct {
	Country           *CountryRecord   `json:"country,omitempty"`
	RegisteredCountry *CountryRecord   `json:"registered_country,omitempty"`
	Continent         *ContinentRecord `json:"continent,omitempty"`
	City              *CityRecord      `json:"city,omitempty"`
	Subdivision       *SubdivisionRecord `json:"subdivision,omitempty"`
	Location          *LocationRecord  `json:"location,omitempty"`
	Postal            *PostalRecord    `json:"postal,omitempty"`
	ASN               *ASNRecord       `json:"asn,omitempty"`
}

type PostalRecord struct {
	Code string `json:"code,omitempty"`
}

type EnrichedServer struct {
	ID                      string         `json:"id"`
	Hostname                string         `json:"hostname,omitempty"`
	IP                      string         `json:"ip,omitempty"`
	CountryShort            string         `json:"countryshort"`
	CountryLong             string         `json:"countrylong"`
	Ping                    string         `json:"ping,omitempty"`
	Speed                   string         `json:"speed,omitempty"`
	Status                  string         `json:"status"`
	FirstSeen               int64          `json:"firstSeen"`
	LastSeen                int64          `json:"lastSeen"`
	LastChanged             int64          `json:"lastChanged"`
	SeenCount               int            `json:"seenCount"`
	MissCount               int            `json:"missCount"`
	ConfigHash              string         `json:"configHash"`
	ContentHash             string         `json:"contentHash"`
	ConfigFilename          string         `json:"configFilename,omitempty"`
	OpenVPNConfigDataBase64 string         `json:"openvpn_configdata_base64,omitempty"`
	MaxMind                 *MaxMindRecord `json:"maxmind,omitempty"`
}

type DataInput struct {
	GeneratedAt    int64              `json:"generatedAt"`
	GeneratedAtISO string             `json:"generatedAtIso"`
	Data           DataContent        `json:"data"`
	Statistics     map[string]any     `json:"statistics"`
}

type DataContent struct {
	Servers   []InputServer     `json:"servers"`
	Countries map[string]string `json:"countries"`
}

type InputServer struct {
	ID                      string `json:"id"`
	Hostname                string `json:"hostname,omitempty"`
	IP                      string `json:"ip,omitempty"`
	CountryShort            string `json:"countryshort"`
	CountryLong             string `json:"countrylong"`
	Ping                    string `json:"ping,omitempty"`
	Speed                   string `json:"speed,omitempty"`
	Status                  string `json:"status"`
	FirstSeen               int64  `json:"firstSeen"`
	LastSeen                int64  `json:"lastSeen"`
	LastChanged             int64  `json:"lastChanged"`
	SeenCount               int    `json:"seenCount"`
	MissCount               int    `json:"missCount"`
	ConfigHash              string `json:"configHash"`
	ContentHash             string `json:"contentHash"`
	ConfigFilename          string `json:"configFilename,omitempty"`
	OpenVPNConfigDataBase64 string `json:"openvpn_configdata_base64,omitempty"`
}

type DataOutput struct {
	GeneratedAt    int64              `json:"generatedAt"`
	GeneratedAtISO string             `json:"generatedAtIso"`
	Data           DataContentOut     `json:"data"`
	Statistics     map[string]any     `json:"statistics"`
	MaxMind        *MaxMindMeta       `json:"maxmind,omitempty"`
}

type DataContentOut struct {
	Servers   []EnrichedServer `json:"servers"`
	Countries map[string]string `json:"countries"`
}

type MaxMindMeta struct {
	GeneratedAt string `json:"generatedAt"`
	Database    string `json:"database"`
}

func Enrich(inputPath, outputPath, maxmindDir string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	var input DataInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("parse input: %w", err)
	}

	countryPath := filepath.Join(maxmindDir, "GeoLite2-Country.mmdb")
	cityPath := filepath.Join(maxmindDir, "GeoLite2-City.mmdb")
	asnPath := filepath.Join(maxmindDir, "GeoLite2-ASN.mmdb")

	countryReader, err := maxminddb.Open(countryPath)
	if err != nil {
		return fmt.Errorf("open country db: %w", err)
	}
	defer countryReader.Close()

	cityReader, err := maxminddb.Open(cityPath)
	if err != nil {
		return fmt.Errorf("open city db: %w", err)
	}
	defer cityReader.Close()

	asnReader, err := maxminddb.Open(asnPath)
	if err != nil {
		return fmt.Errorf("open asn db: %w", err)
	}
	defer asnReader.Close()

	enriched := make([]EnrichedServer, 0, len(input.Data.Servers))

	for _, s := range input.Data.Servers {
		server := EnrichedServer{
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
		}

		if s.IP != "" {
			server.MaxMind = buildMaxMindRecord(s.IP, countryReader, cityReader, asnReader)
		}

		enriched = append(enriched, server)
	}

	output := DataOutput{
		GeneratedAt:    input.GeneratedAt,
		GeneratedAtISO: input.GeneratedAtISO,
		Data: DataContentOut{
			Servers:   enriched,
			Countries: input.Data.Countries,
		},
		Statistics: input.Statistics,
		MaxMind: &MaxMindMeta{
			GeneratedAt: input.GeneratedAtISO,
			Database:    "GeoLite2",
		},
	}

	return atomicWriteJSON(outputPath, output)
}

func buildMaxMindRecord(ip string, countryReader, cityReader, asnReader *maxminddb.Reader) *MaxMindRecord {
	record := &MaxMindRecord{}

	netIP := net.ParseIP(ip)
	if netIP == nil {
		return nil
	}

	// Country lookup
	var countryData struct {
		Country struct {
			IsoCode string `maxminddb:"iso_code"`
			Name    struct {
				En string `maxminddb:"en"`
			} `maxminddb:"names"`
			IsInEU bool `maxminddb:"is_in_european_union"`
		} `maxminddb:"country"`
		RegisteredCountry struct {
			IsoCode string `maxminddb:"iso_code"`
			Name    struct {
				En string `maxminddb:"en"`
			} `maxminddb:"names"`
		} `maxminddb:"registered_country"`
		Continent struct {
			Code string `maxminddb:"code"`
			Name struct {
				En string `maxminddb:"en"`
			} `maxminddb:"names"`
		} `maxminddb:"continent"`
	}

	_ = countryReader.Lookup(netIP, &countryData)
	if countryData.Country.IsoCode != "" {
		record.Country = &CountryRecord{
			IsoCode: countryData.Country.IsoCode,
			Name:    countryData.Country.Name.En,
			Names:   map[string]string{"en": countryData.Country.Name.En},
			IsInEU:  countryData.Country.IsInEU,
		}
	}
	if countryData.RegisteredCountry.IsoCode != "" {
		record.RegisteredCountry = &CountryRecord{
			IsoCode: countryData.RegisteredCountry.IsoCode,
			Name:    countryData.RegisteredCountry.Name.En,
			Names:   map[string]string{"en": countryData.RegisteredCountry.Name.En},
		}
	}
	if countryData.Continent.Code != "" {
		record.Continent = &ContinentRecord{
			Code:  countryData.Continent.Code,
			Name:  countryData.Continent.Name.En,
			Names: map[string]string{"en": countryData.Continent.Name.En},
		}
	}

	// City lookup
	var cityData struct {
		City struct {
			Name struct {
				En string `maxminddb:"en"`
			} `maxminddb:"names"`
			GeoNameID uint `maxminddb:"geoname_id"`
		} `maxminddb:"city"`
		Subdivisions []struct {
			IsoCode string `maxminddb:"iso_code"`
			Name    struct {
				En string `maxminddb:"en"`
			} `maxminddb:"names"`
			GeoNameID uint `maxminddb:"geoname_id"`
		} `maxminddb:"subdivisions"`
		Location struct {
			Latitude  float64 `maxminddb:"latitude"`
			Longitude float64 `maxminddb:"longitude"`
			TimeZone  string  `maxminddb:"time_zone"`
		} `maxminddb:"location"`
		Postal struct {
			Code string `maxminddb:"code"`
		} `maxminddb:"postal"`
	}

	_ = cityReader.Lookup(netIP, &cityData)
	if cityData.City.Name.En != "" {
		record.City = &CityRecord{
			Name:      cityData.City.Name.En,
			Names:     map[string]string{"en": cityData.City.Name.En},
			GeoNameID: cityData.City.GeoNameID,
		}
	}
	if len(cityData.Subdivisions) > 0 {
		sd := cityData.Subdivisions[0]
		record.Subdivision = &SubdivisionRecord{
			IsoCode:   sd.IsoCode,
			Name:      sd.Name.En,
			Names:     map[string]string{"en": sd.Name.En},
			GeoNameID: sd.GeoNameID,
		}
	}
	if cityData.Location.Latitude != 0 || cityData.Location.Longitude != 0 {
		record.Location = &LocationRecord{
			Latitude:  cityData.Location.Latitude,
			Longitude: cityData.Location.Longitude,
			TimeZone:  cityData.Location.TimeZone,
		}
	}
	if cityData.Postal.Code != "" {
		record.Postal = &PostalRecord{Code: cityData.Postal.Code}
	}

	// ASN lookup
	var asnData struct {
		AutonomousSystemNumber       uint   `maxminddb:"autonomous_system_number"`
		AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
	}

	_ = asnReader.Lookup(netIP, &asnData)
	if asnData.AutonomousSystemNumber != 0 {
		record.ASN = &ASNRecord{
			AutonomousSystemNumber:       asnData.AutonomousSystemNumber,
			AutonomousSystemOrganization: asnData.AutonomousSystemOrganization,
		}
	}

	if record.Country == nil && record.City == nil && record.ASN == nil {
		return nil
	}
	return record
}

func atomicWriteJSON(path string, data any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, jsonData, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

type openvpnConfig struct {
	remoteHost string
	remotePort int
	isUDP      bool
	cipher     string
	auth       string
	ca         string
	cert       string
	key        string
	tlsCrypt   string
}

func BuildMihomoConfig(dataPath, outputPath string) error {
	// Collect servers from data.json
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return fmt.Errorf("read data: %w", err)
	}

	var input DataInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("parse data: %w", err)
	}

	// Also load state file which contains all servers (including inactive)
	// State file path is derived from data.json path: ../state/servers.json
	statePath := filepath.Join(filepath.Dir(dataPath), "..", "state", "servers.json")
	stateServers := loadStateServers(statePath)

	// Merge: data.json servers + state servers (state has inactive ones too)
	seen := make(map[string]bool)
	merged := make([]InputServer, 0, len(input.Data.Servers)+len(stateServers))

	for _, s := range input.Data.Servers {
		key := s.ID
		if key == "" {
			key = s.Hostname
		}
		if key == "" {
			key = s.IP
		}
		if key != "" {
			seen[key] = true
		}
		merged = append(merged, s)
	}

	for _, s := range stateServers {
		key := s.ID
		if key == "" {
			key = s.Hostname
		}
		if key == "" {
			key = s.IP
		}
		if key == "" || seen[key] {
			continue
		}
		merged = append(merged, s)
	}

	var sb strings.Builder
	sb.WriteString("proxies:\n")

	for _, s := range merged {
		if s.OpenVPNConfigDataBase64 == "" {
			continue
		}

		config, err := decodeAndParseConfig(s.OpenVPNConfigDataBase64)
		if err != nil {
			continue
		}

		name := buildProxyName(s)
		sb.WriteString(fmt.Sprintf("  - name: %q\n", name))
		sb.WriteString("    type: openvpn\n")
		sb.WriteString(fmt.Sprintf("    server: %q\n", config.remoteHost))
		sb.WriteString(fmt.Sprintf("    port: %d\n", config.remotePort))
		sb.WriteString(fmt.Sprintf("    udp: %v\n", config.isUDP))
		if config.cipher != "" {
			sb.WriteString(fmt.Sprintf("    cipher: %q\n", config.cipher))
		}
		if config.auth != "" {
			sb.WriteString(fmt.Sprintf("    auth: %q\n", config.auth))
		}
		if config.ca != "" {
			sb.WriteString("    ca: |-\n")
			for line := range strings.SplitSeq(config.ca, "\n") {
				sb.WriteString(fmt.Sprintf("      %s\n", line))
			}
		}
		if config.cert != "" {
			sb.WriteString("    cert: |-\n")
			for line := range strings.SplitSeq(config.cert, "\n") {
				sb.WriteString(fmt.Sprintf("      %s\n", line))
			}
		}
		if config.key != "" {
			sb.WriteString("    key: |-\n")
			for line := range strings.SplitSeq(config.key, "\n") {
				sb.WriteString(fmt.Sprintf("      %s\n", line))
			}
		}
		if config.tlsCrypt != "" {
			sb.WriteString("    tls-crypt: |-\n")
			for line := range strings.SplitSeq(config.tlsCrypt, "\n") {
				sb.WriteString(fmt.Sprintf("      %s\n", line))
			}
		}
	}

	return atomicWriteText(outputPath, sb.String())
}

// stateFileData represents the state file structure.
type stateFileData struct {
	Servers map[string]*stateServerEntry `json:"servers"`
}

type stateServerEntry struct {
	ID                      string `json:"id"`
	Hostname                string `json:"hostname,omitempty"`
	IP                      string `json:"ip,omitempty"`
	CountryShort            string `json:"countryshort"`
	CountryLong             string `json:"countrylong"`
	Status                  string `json:"status"`
	OpenVPNConfigDataBase64 string `json:"openvpn_configdata_base64,omitempty"`
}

// loadStateServers reads the state file and returns servers that have config data.
func loadStateServers(statePath string) []InputServer {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}

	var sf stateFileData
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil
	}

	var servers []InputServer
	for _, s := range sf.Servers {
		if s.OpenVPNConfigDataBase64 == "" {
			continue
		}
		// Skip pruned servers (shouldn't exist in state, but guard anyway)
		if s.Status == "pruned" {
			continue
		}
		servers = append(servers, InputServer{
			ID:                      s.ID,
			Hostname:                s.Hostname,
			IP:                      s.IP,
			CountryShort:            s.CountryShort,
			CountryLong:             s.CountryLong,
			OpenVPNConfigDataBase64: s.OpenVPNConfigDataBase64,
		})
	}
	return servers
}

func decodeAndParseConfig(encoded string) (*openvpnConfig, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	config := &openvpnConfig{remotePort: 1194, isUDP: true}
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "remote "):
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				config.remoteHost = parts[1]
			}
			if len(parts) >= 3 {
				port := 0
				fmt.Sscanf(parts[2], "%d", &port)
				if port > 0 {
					config.remotePort = port
				}
			}
		case strings.HasPrefix(line, "proto "):
			proto := strings.TrimSpace(strings.TrimPrefix(line, "proto"))
			config.isUDP = proto == "udp"
		case strings.HasPrefix(line, "cipher "):
			config.cipher = strings.TrimSpace(strings.TrimPrefix(line, "cipher"))
		case strings.HasPrefix(line, "auth "):
			config.auth = strings.TrimSpace(strings.TrimPrefix(line, "auth"))
		case strings.HasPrefix(line, "<ca>"):
			config.ca = extractBlock(lines, "ca")
		case strings.HasPrefix(line, "<cert>"):
			config.cert = extractBlock(lines, "cert")
		case strings.HasPrefix(line, "<key>"):
			config.key = extractBlock(lines, "key")
		case strings.HasPrefix(line, "<tls-crypt>"):
			config.tlsCrypt = extractBlock(lines, "tls-crypt")
		}
	}

	return config, nil
}

func extractBlock(lines []string, tag string) string {
	inBlock := false
	var content strings.Builder
	startTag := "<" + tag + ">"
	endTag := "</" + tag + ">"

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == startTag {
			inBlock = true
			continue
		}
		if trimmed == endTag {
			break
		}
		if inBlock {
			content.WriteString(line)
			content.WriteString("\n")
		}
	}
	return strings.TrimSpace(content.String())
}

func buildProxyName(s InputServer) string {
	country := s.CountryShort
	if country == "" {
		country = "XX"
	}
	hostname := s.Hostname
	if hostname == "" {
		hostname = s.IP
	}
	return fmt.Sprintf("%s %s", country, hostname)
}

func atomicWriteText(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
