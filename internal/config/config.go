package config

import (
"fmt"
"os"
"path/filepath"

"github.com/BurntSushi/toml"
)

type DaemonConfig struct {
BindAddress string `toml:"bind_address"`
Port        int    `toml:"port"`
LogLevel    string `toml:"log_level"`
Security    struct {
InternalAPIToken string `toml:"internal_api_token"`
} `toml:"security"`
}

type SourceConfig struct {
Name     string `toml:"name"`
Endpoint string `toml:"endpoint"`
Color    string `toml:"color"`
CanRead  bool   `toml:"can_read"`
CanWrite bool   `toml:"can_write"`

Auth struct {
Type   string `toml:"type"`
Header string `toml:"header"`
EnvVar string `toml:"env_var"`
} `toml:"auth"`

Mapping map[string]string `toml:"mapping"`
}

type DestinationConfig struct {
Name string `toml:"name"`
Type string `toml:"type"`

Filepath string `toml:"filepath"`
Table    string `toml:"table"`

BaseURL string `toml:"base_url"`
Auth    struct {
EnvVar string `toml:"env_var"`
} `toml:"auth"`

Schema struct {
RequiredFields []string `toml:"required_fields"`
} `toml:"schema"`
}

func LoadDaemonConfig(path string) (*DaemonConfig, error) {
var cfg DaemonConfig

if _, err := os.Stat(path); err != nil {
return nil, fmt.Errorf("failed to load daemon.toml: %w", err)
}

if _, err := toml.DecodeFile(path, &cfg); err != nil {
return nil, fmt.Errorf("failed to load daemon.toml: %w", err)
}

return &cfg, nil
}

func LoadSourceConfigs(dir string) ([]SourceConfig, error) {
files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
if err != nil {
return nil, fmt.Errorf("failed to scan source configs: %w", err)
}

var configs []SourceConfig

for _, file := range files {
var cfg SourceConfig
if _, err := toml.DecodeFile(file, &cfg); err != nil {
return nil, fmt.Errorf("failed to load source config %s: %w", file, err)
}
configs = append(configs, cfg)
}

return configs, nil
}

func LoadDestinationConfigs(dir string) ([]DestinationConfig, error) {
files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
if err != nil {
return nil, fmt.Errorf("failed to scan destination configs: %w", err)
}

var configs []DestinationConfig

for _, file := range files {
var cfg DestinationConfig
if _, err := toml.DecodeFile(file, &cfg); err != nil {
return nil, fmt.Errorf("failed to load destination config %s: %w", file, err)
}
configs = append(configs, cfg)
}

return configs, nil
}
