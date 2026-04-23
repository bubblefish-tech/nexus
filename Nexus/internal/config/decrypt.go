// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package config

import (
	"fmt"
	"log/slog"
	"path/filepath"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

// LoadWithKey loads the configuration and decrypts any ENC:v1: encrypted field
// values using the config sub-key from mkm before secret references are resolved.
// If mkm is nil or not enabled, it behaves identically to Load().
func LoadWithKey(configDir string, logger *slog.Logger, mkm *nexuscrypto.MasterKeyManager) (*Config, error) {
	cfg, err := loadDaemonTOML(filepath.Join(configDir, "daemon.toml"), logger)
	if err != nil {
		return nil, err
	}

	sources, err := loadSources(filepath.Join(configDir, "sources"), logger)
	if err != nil {
		return nil, err
	}
	cfg.Sources = sources

	dests, err := loadDestinations(filepath.Join(configDir, "destinations"), logger)
	if err != nil {
		return nil, err
	}
	cfg.Destinations = dests

	if mkm != nil && mkm.IsEnabled() {
		configKey := mkm.SubKey("nexus-config-key-v1")
		if err := decryptAllConfigStrings(cfg, configKey); err != nil {
			return nil, fmt.Errorf("config: decrypt encrypted fields: %w", err)
		}
	}

	if err := resolveAndValidate(cfg, configDir, logger); err != nil {
		return nil, err
	}

	return cfg, nil
}

// decryptAllConfigStrings decrypts ENC:v1: values in the known sensitive raw
// string fields of cfg. This runs before resolveAndValidate so that secret
// references (env:/file:/literal) are resolved from plaintext, not ciphertext.
func decryptAllConfigStrings(cfg *Config, key [32]byte) error {
	// Daemon-level sensitive fields.
	daemonFields := []*string{
		&cfg.Daemon.AdminToken,
		&cfg.Daemon.MCP.APIKey,
		&cfg.Daemon.Review.ListToken,
		&cfg.Daemon.Review.ReadToken,
		&cfg.Daemon.Embedding.APIKey,
		&cfg.Daemon.Embedding.URL,
		&cfg.Daemon.WAL.Integrity.MacKeyFile,
		&cfg.Daemon.WAL.Encryption.KeyFile,
		&cfg.Daemon.Audit.Integrity.MacKeyFile,
		&cfg.Daemon.Audit.Encryption.KeyFile,
		&cfg.Daemon.Signing.KeyFile,
		&cfg.Daemon.TLS.CertFile,
		&cfg.Daemon.TLS.KeyFile,
		&cfg.Daemon.OAuth.PrivateKeyFile,
	}
	for _, fp := range daemonFields {
		dec, err := nexuscrypto.DecryptField(*fp, key)
		if err != nil {
			return err
		}
		*fp = dec
	}

	// Per-source sensitive fields.
	for _, src := range cfg.Sources {
		dec, err := nexuscrypto.DecryptField(src.APIKey, key)
		if err != nil {
			return fmt.Errorf("source %q api_key: %w", src.Name, err)
		}
		src.APIKey = dec
	}

	// Per-destination sensitive fields.
	for _, dst := range cfg.Destinations {
		for _, fp := range []*string{&dst.DSN, &dst.APIKey, &dst.URL, &dst.ConnectionString} {
			dec, err := nexuscrypto.DecryptField(*fp, key)
			if err != nil {
				return fmt.Errorf("destination %q: %w", dst.Name, err)
			}
			*fp = dec
		}
	}

	return nil
}
