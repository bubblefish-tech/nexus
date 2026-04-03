// Package config loads BubbleFish daemon and source configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DaemonConfig is loaded from ~/.bubblefish/daemon.toml (stored as JSON for simplicity).
type DaemonConfig struct {
	Port        int              `json:"port"`         // default 8080
	APIToken    string           `json:"api_token"`    // admin token for /api/* endpoints
	WALDir      string           `json:"wal_dir"`
	WALMaxBytes int64            `json:"wal_max_bytes"`
	QueueCap    int              `json:"queue_capacity"`
	Destinations []DestConfig   `json:"destinations"`
}

type DestConfig struct {
	Name    string            `json:"name"`
	Options map[string]string `json:"options"`
}

// SourceConfig is loaded from ~/.bubblefish/sources/<name>.json
type SourceConfig struct {
	Name            string            `json:"name"`
	APIKey          string            `json:"api_key"`
	MaxPayloadBytes int64             `json:"max_payload_bytes"` // default 10MB
	RateLimit       RateLimitConfig   `json:"rate_limit"`
	Mappings        []MappingRule     `json:"mappings"`
	Transforms      []TransformRule   `json:"transforms"`
	ResponseFilter  FilterConfig      `json:"response_filter"`
	DedupeWindow    string            `json:"dedupe_window"` // e.g. "5m"
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `json:"requests_per_second"` // token bucket fill rate
	Burst             int     `json:"burst"`               // bucket capacity
}

type MappingRule struct {
	From string `json:"from"` // jsonpath source e.g. "message.content"
	To   string `json:"to"`   // target field name e.g. "text"
}

type TransformRule struct {
	Field    string   `json:"field"`
	Function string   `json:"function"` // trim, concat, coalesce, conditional
	Args     []string `json:"args"`
}

type FilterConfig struct {
	IncludeFields []string `json:"include_fields"`
	ExcludeFields []string `json:"exclude_fields"`
}

// LoadDaemon loads daemon config from path (JSON).
func LoadDaemon(path string) (*DaemonConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read daemon config %q: %w", path, err)
	}
	var cfg DaemonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse daemon config: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.QueueCap == 0 {
		cfg.QueueCap = 1000
	}
	if cfg.WALDir == "" {
		cfg.WALDir = filepath.Join(configDir(), "wal")
	}
	return &cfg, nil
}

// LoadSource loads a source config by name from ~/.bubblefish/sources/<name>.json
func LoadSource(name string) (*SourceConfig, error) {
	path := filepath.Join(configDir(), "sources", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read source %q: %w", name, err)
	}
	var cfg SourceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse source %q: %w", name, err)
	}
	if cfg.MaxPayloadBytes == 0 {
		cfg.MaxPayloadBytes = 10 * 1024 * 1024 // 10 MB
	}
	if cfg.Name == "" {
		cfg.Name = name
	}
	return &cfg, nil
}

// LoadAllSources reads every *.json file from ~/.bubblefish/sources/
func LoadAllSources(dir string) (map[string]*SourceConfig, error) {
	if dir == "" {
		dir = filepath.Join(configDir(), "sources")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*SourceConfig{}, nil
		}
		return nil, fmt.Errorf("config: read sources dir: %w", err)
	}

	sources := make(map[string]*SourceConfig)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := e.Name()[:len(e.Name())-5] // strip .json
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: read %q: %w", path, err)
		}
		var cfg SourceConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("config: parse %q: %w", path, err)
		}
		if cfg.MaxPayloadBytes == 0 {
			cfg.MaxPayloadBytes = 10 * 1024 * 1024
		}
		if cfg.Name == "" {
			cfg.Name = name
		}
		sources[name] = &cfg
	}
	return sources, nil
}

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".bubblefish"
	}
	return filepath.Join(home, ".bubblefish")
}

// DefaultDaemonConfigPath returns ~/.bubblefish/daemon.json
func DefaultDaemonConfigPath() string {
	return filepath.Join(configDir(), "daemon.json")
}
