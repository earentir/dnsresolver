package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// FileName is the canonical name of the configuration file.
	FileName = "dnsplane.json"
	// systemConfigPath is the location checked last when resolving the config.
	systemConfigPath = "/etc/" + FileName
)

// FileLocations describes the JSON data files used by dnsplane.
type FileLocations struct {
	DNSServerFile  string `json:"dnsserver_file"`
	DNSRecordsFile string `json:"dnsrecords_file"`
	CacheFile      string `json:"cache_file"`
}

// DNSRecordSettings mirrors record handling settings persisted in the config.
type DNSRecordSettings struct {
	AutoBuildPTRFromA bool `json:"auto_build_ptr_from_a"`
	ForwardPTRQueries bool `json:"forward_ptr_queries"`
	AddUpdatesRecords bool `json:"add_updates_records,omitempty"`
}

// Config captures all persisted settings for dnsplane.
type Config struct {
	FallbackServerIP   string            `json:"fallback_server_ip"`
	FallbackServerPort string            `json:"fallback_server_port"`
	Timeout            int               `json:"timeout"`
	DNSPort            string            `json:"dns_port"`
	RESTPort           string            `json:"rest_port"`
	APIEnabled         bool              `json:"api_enabled"`
	CacheRecords       bool              `json:"cache_records"`
	ClientSocketPath   string            `json:"client_socket_path"`
	ClientTCPAddress   string            `json:"client_tcp_address"`
	FileLocations      FileLocations     `json:"file_locations"`
	DNSRecordSettings  DNSRecordSettings `json:"DNSRecordSettings"`
}

// Loaded contains the configuration together with metadata about the source file.
type Loaded struct {
	Path    string
	Created bool
	Config  Config
}

// Load resolves the dnsplane configuration file, creating a default one if
// necessary, and returns the parsed configuration alongside metadata.
func Load() (*Loaded, error) {
	candidates, err := candidatePaths()
	if err != nil {
		return nil, err
	}

	for _, path := range candidates {
		cfg, err := readConfig(path)
		if err == nil {
			cfg.applyDefaults(filepath.Dir(path))
			return &Loaded{Path: path, Config: *cfg}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
		}
	}

	defaultDir, err := defaultConfigDir()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		return nil, fmt.Errorf("config: ensure config directory %s: %w", defaultDir, err)
	}
	defaultPath := filepath.Join(defaultDir, FileName)
	cfg := defaultConfig(defaultDir)
	if err := writeConfig(defaultPath, cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults(defaultDir)
	return &Loaded{Path: defaultPath, Created: true, Config: *cfg}, nil
}

// Read loads and normalises configuration from the specified path without
// searching other locations.
func Read(path string) (*Config, error) {
	cfg, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	cfg.applyDefaults(filepath.Dir(path))
	return cfg, nil
}

// Save writes the supplied configuration back to the given path.
func Save(path string, cfg Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: ensure config directory %s: %w", dir, err)
	}
	cfg.applyDefaults(dir)
	return writeConfig(path, &cfg)
}

// Normalize ensures derived fields like file paths are populated for the
// provided configuration relative to the supplied directory.
func (c *Config) Normalize(configDir string) {
	c.applyDefaults(configDir)
}

func candidatePaths() ([]string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("config: determine executable path: %w", err)
	}
	execDir := filepath.Dir(execPath)

	var paths []string
	paths = appendIfMissing(paths, filepath.Join(execDir, FileName))

	if userPath, err := userConfigPath(); err == nil && userPath != "" {
		paths = appendIfMissing(paths, userPath)
	}

	paths = appendIfMissing(paths, systemConfigPath)
	return paths, nil
}

func userConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: determine user config dir: %w", err)
	}
	return filepath.Join(dir, "dnsplane", FileName), nil
}

func defaultConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: determine user config dir: %w", err)
	}
	return filepath.Join(dir, "dnsplane"), nil
}

func readConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("config: file %s is empty", path)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if legacy := extractLegacyRecordSettings(data); legacy != nil {
		cfg.DNSRecordSettings = *legacy
	}
	return &cfg, nil
}

func writeConfig(path string, cfg *Config) error {
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal config: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

func defaultConfig(baseDir string) *Config {
	return &Config{
		FallbackServerIP:   "1.1.1.1",
		FallbackServerPort: "53",
		Timeout:            2,
		DNSPort:            "53",
		RESTPort:           "8080",
		APIEnabled:         false,
		CacheRecords:       true,
		ClientSocketPath:   defaultSocketPath(),
		ClientTCPAddress:   "0.0.0.0:8053",
		FileLocations: FileLocations{
			DNSServerFile:  filepath.Join(baseDir, "dnsservers.json"),
			DNSRecordsFile: filepath.Join(baseDir, "dnsrecords.json"),
			CacheFile:      filepath.Join(baseDir, "dnscache.json"),
		},
		DNSRecordSettings: DNSRecordSettings{
			AutoBuildPTRFromA: true,
			ForwardPTRQueries: false,
		},
	}
}

func (c *Config) applyDefaults(configDir string) {
	if c.FallbackServerIP == "" {
		c.FallbackServerIP = "1.1.1.1"
	}
	if c.FallbackServerPort == "" {
		c.FallbackServerPort = "53"
	}
	if c.DNSPort == "" {
		c.DNSPort = "53"
	}
	if c.RESTPort == "" {
		c.RESTPort = "8080"
	}
	if c.ClientSocketPath == "" {
		c.ClientSocketPath = defaultSocketPath()
	}
	if c.ClientTCPAddress == "" {
		c.ClientTCPAddress = "0.0.0.0:8053"
	}

	c.FileLocations.DNSServerFile = ensureAbsolutePath(configDir, c.FileLocations.DNSServerFile, "dnsservers.json")
	c.FileLocations.DNSRecordsFile = ensureAbsolutePath(configDir, c.FileLocations.DNSRecordsFile, "dnsrecords.json")
	c.FileLocations.CacheFile = ensureAbsolutePath(configDir, c.FileLocations.CacheFile, "dnscache.json")
}

func appendIfMissing(paths []string, candidate string) []string {
	for _, existing := range paths {
		if existing == candidate {
			return paths
		}
	}
	return append(paths, candidate)
}

func ensureAbsolutePath(configDir, value, fallbackName string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return filepath.Join(configDir, fallbackName)
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(configDir, value)
}

func defaultSocketPath() string {
	return filepath.Join(os.TempDir(), "dnsplane.socket")
}

func extractLegacyRecordSettings(data []byte) *DNSRecordSettings {
	type legacy struct {
		DNSRecordSettings *DNSRecordSettings `json:"DNSRecordSettings"`
	}
	var l legacy
	if err := json.Unmarshal(data, &l); err != nil {
		return nil
	}
	return l.DNSRecordSettings
}
