package maxmind

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildMihomoConfigEmitsOpenVPNProto(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "json", "data.json")
	outPath := filepath.Join(dir, "mihomo.yaml")

	ovpn := strings.Join([]string{
		"client",
		"dev tun",
		"proto tcp-client",
		"remote vpn.example.test 443",
		"cipher AES-128-CBC",
		"auth SHA1",
		"<ca>",
		"-----BEGIN CERTIFICATE-----",
		"MIIB",
		"-----END CERTIFICATE-----",
		"</ca>",
		"<cert>",
		"-----BEGIN CERTIFICATE-----",
		"MIIB",
		"-----END CERTIFICATE-----",
		"</cert>",
		"<key>",
		"-----BEGIN PRIVATE KEY-----",
		"MIIB",
		"-----END PRIVATE KEY-----",
		"</key>",
	}, "\n")

	input := DataOutput{
		Data: DataContentOut{
			Servers: []EnrichedServer{
				{
					InputServer: InputServer{
						ID:                      "server-1",
						Hostname:                "vpn.example.test",
						CountryShort:            "US",
						OpenVPNConfigDataBase64: base64.StdEncoding.EncodeToString([]byte(ovpn)),
					},
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dataPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, raw, 0644); err != nil {
		t.Fatal(err)
	}

	if err := BuildMihomoConfig(dataPath, outPath); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `proto: "tcp"`) {
		t.Fatalf("mihomo config does not contain tcp proto:\n%s", out)
	}
}

func TestBuildProxyName(t *testing.T) {
	tests := []struct {
		name   string
		server EnrichedServer
		want   string
	}{
		{
			name: "no maxmind falls back to country hostname",
			server: EnrichedServer{
				InputServer: InputServer{CountryShort: "US", Hostname: "vpn.example.test"},
			},
			want: "US vpn.example.test",
		},
		{
			name: "asn label inserted before hostname",
			server: EnrichedServer{
				InputServer: InputServer{CountryShort: "JP", Hostname: "vpn.example.test"},
				MaxMind: &MaxMindRecord{
					ASN: &ASNRecord{AutonomousSystemNumber: 2516},
				},
			},
			want: "JP ASN2516 vpn.example.test",
		},
		{
			name: "missing country uses XX",
			server: EnrichedServer{
				InputServer: InputServer{Hostname: "vpn.example.test"},
				MaxMind: &MaxMindRecord{
					ASN: &ASNRecord{AutonomousSystemNumber: 1},
				},
			},
			want: "XX ASN1 vpn.example.test",
		},
		{
			name: "missing hostname falls back to ip",
			server: EnrichedServer{
				InputServer: InputServer{CountryShort: "JP", IP: "203.0.113.7"},
				MaxMind: &MaxMindRecord{
					ASN: &ASNRecord{AutonomousSystemNumber: 2516},
				},
			},
			want: "JP ASN2516 203.0.113.7",
		},
		{
			name: "zero asn number falls back",
			server: EnrichedServer{
				InputServer: InputServer{CountryShort: "JP", Hostname: "vpn.example.test"},
				MaxMind:     &MaxMindRecord{ASN: &ASNRecord{}},
			},
			want: "JP vpn.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildProxyName(tt.server); got != tt.want {
				t.Fatalf("buildProxyName() = %q, want %q", got, tt.want)
			}
		})
	}
}
