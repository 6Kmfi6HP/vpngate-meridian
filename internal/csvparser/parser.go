package csvparser

import (
	"regexp"
	"strings"
)

// Server represents a VPN server entry parsed from the API CSV response.
type Server struct {
	Hostname             string `json:"hostname"`
	IP                   string `json:"ip"`
	Speed                string `json:"speed"`
	Ping                 string `json:"ping"`
	CountryLong          string `json:"countrylong"`
	CountryShort         string `json:"countryshort"`
	NumSessions          string `json:"numsessions"`
	OpenVPNConfigDataBase64 string `json:"openvpn_configdata_base64"`
}

type ParseResult struct {
	Servers   []Server
	Countries map[string]string
}

var endMarker = regexp.MustCompile(`^END_OF_OVPN`)

// Parse splits a VPN Gate CSV API response into structured server entries.
func Parse(data string) (*ParseResult, error) {
	lines := strings.Split(data, "\n")
	if len(lines) < 3 {
		return &ParseResult{Servers: []Server{}, Countries: make(map[string]string)}, nil
	}

	headerLine := lines[1]
	// Clean the header: first char may have BOM or special prefix
	if len(headerLine) > 0 {
		headerLine = headerLine[1:]
	}
	headerLine = strings.TrimRight(headerLine, "\r")
	headers := strings.Split(headerLine, ",")

	servers := make([]Server, 0)
	countries := make(map[string]string)

	// Process data rows (skip first 2 lines and last 2 lines)
	endIdx := len(lines) - 2
	if endIdx < 2 {
		endIdx = len(lines)
	}

	for i := 2; i < endIdx; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < len(headers) {
			continue
		}

		server := Server{}
		for j, h := range headers {
			h = strings.TrimSpace(h)
			val := strings.TrimSpace(fields[j])
			switch h {
			case "HostName":
				server.Hostname = val
			case "IP":
				server.IP = val
			case "Speed":
				server.Speed = val
			case "Ping":
				server.Ping = val
			case "CountryLong":
				server.CountryLong = val
			case "CountryShort":
				server.CountryShort = val
			case "NumSessions":
				server.NumSessions = val
			case "OpenVPN_ConfigData_Base64":
				server.OpenVPNConfigDataBase64 = val
			}
		}

		// Build countries map
		if server.CountryShort != "" && server.CountryLong != "" {
			countries[strings.ToLower(server.CountryShort)] = server.CountryLong
		}

		servers = append(servers, server)
	}

	return &ParseResult{Servers: servers, Countries: countries}, nil
}
