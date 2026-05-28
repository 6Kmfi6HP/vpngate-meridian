package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/6Kmfi6HP/vpn-meridian/internal/state"
)

func (w *Writer) GenerateReadme(result *state.MergeResult) error {
	var content string
	content += "# VPN Meridian Data\n\n"
	content += "This branch contains generated VPN Meridian output. The source code lives on the main branch.\n\n"
	content += "Last generated: " + result.GeneratedAtISO + "\n\n"
	content += "Active servers: " + strconv.Itoa(len(result.Servers)) + "\n\n"
	content += "Machine-readable data: [json/data.json](./json/data.json)\n\n"
	content += "| Hostname | IP Address | Ping | Speed | Country | OpenVPN Config |\n"
	content += "|----------|------------|------|-------|---------|----------------|\n"

	for _, s := range result.Servers {
		speedMbps := formatSpeedMbps(s.Speed)
		configPath := "configs/" + s.ConfigFilename
		content += fmt.Sprintf("| %s | %s | %s | %s Mbps | %s | [Download](./%s) |\n",
			s.Hostname, s.IP, s.Ping, speedMbps, s.CountryLong, configPath)
	}

	return os.WriteFile(
		filepath.Join(w.OutputDir, "README.md"),
		[]byte(content),
		0644,
	)
}
