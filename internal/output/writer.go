package output

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/6Kmfi6HP/vpngate-scraper/internal/state"
)

type Writer struct {
	OutputDir string
	ConfigDir string
	JSONDir   string
}

func NewWriter(outputDir string) *Writer {
	return &Writer{
		OutputDir: outputDir,
		ConfigDir: filepath.Join(outputDir, "configs"),
		JSONDir:   filepath.Join(outputDir, "json"),
	}
}

func (w *Writer) EnsureDirectories() error {
	for _, dir := range []string{w.OutputDir, w.ConfigDir, w.JSONDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

func (w *Writer) WriteJSONFile(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(value, "", "    ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (w *Writer) SaveVpnConfigs(servers []state.PublishedServer) (int, error) {
	if err := os.MkdirAll(w.ConfigDir, 0755); err != nil {
		return 0, err
	}

	written := make(map[string]struct{})
	count := 0

	for _, s := range servers {
		if s.OpenVPNConfigDataBase64 == "" || s.ConfigFilename == "" {
			continue
		}

		decoded, err := base64.StdEncoding.DecodeString(s.OpenVPNConfigDataBase64)
		if err != nil {
			continue
		}

		configPath := filepath.Join(w.ConfigDir, s.ConfigFilename)
		if err := os.WriteFile(configPath, decoded, 0644); err != nil {
			continue
		}

		written[s.ConfigFilename] = struct{}{}
		count++
	}

	entries, err := os.ReadDir(w.ConfigDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ovpn") {
				continue
			}
			if _, ok := written[e.Name()]; !ok {
				os.Remove(filepath.Join(w.ConfigDir, e.Name()))
			}
		}
	}

	return count, nil
}

type dataOutput struct {
	GeneratedAt    int64               `json:"generatedAt"`
	GeneratedAtISO string              `json:"generatedAtIso"`
	Data           dataWrapper         `json:"data"`
	Statistics     *state.Statistics   `json:"statistics"`
}

type dataWrapper struct {
	Servers   []state.PublishedServer `json:"servers"`
	Countries map[string]string       `json:"countries"`
}

func (w *Writer) SaveData(result *state.MergeResult) error {
	out := dataOutput{
		GeneratedAt:    result.GeneratedAt,
		GeneratedAtISO: result.GeneratedAtISO,
		Data: dataWrapper{
			Servers:   result.Servers,
			Countries: result.Countries,
		},
		Statistics: result.Statistics,
	}

	return w.WriteJSONFile(filepath.Join(w.JSONDir, "data.json"), out)
}

type changesOutput struct {
	GeneratedAt    int64          `json:"generatedAt"`
	GeneratedAtISO string         `json:"generatedAtIso"`
	Summary        changesSummary `json:"summary"`
	Changes        *state.Changes `json:"changes"`
}

type changesSummary struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Recovered int `json:"recovered"`
	Missing   int `json:"missing"`
	Inactive  int `json:"inactive"`
	Pruned    int `json:"pruned"`
	Unchanged int `json:"unchanged"`
}

func (w *Writer) SaveChanges(result *state.MergeResult) error {
	out := changesOutput{
		GeneratedAt:    result.GeneratedAt,
		GeneratedAtISO: result.GeneratedAtISO,
		Summary: changesSummary{
			Added:     len(result.Changes.Added),
			Updated:   len(result.Changes.Updated),
			Recovered: len(result.Changes.Recovered),
			Missing:   len(result.Changes.Missing),
			Inactive:  len(result.Changes.Inactive),
			Pruned:    len(result.Changes.Pruned),
			Unchanged: result.Changes.UnchangedCount,
		},
		Changes: result.Changes,
	}

	return w.WriteJSONFile(filepath.Join(w.JSONDir, "changes.json"), out)
}

func (w *Writer) SaveState(path string, stateFile *state.StateFile) error {
	return w.WriteJSONFile(path, stateFile)
}
