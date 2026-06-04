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

	input := DataInput{
		Data: DataContent{
			Servers: []InputServer{
				{
					ID:                      "server-1",
					Hostname:                "vpn.example.test",
					CountryShort:            "US",
					OpenVPNConfigDataBase64: base64.StdEncoding.EncodeToString([]byte(ovpn)),
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
