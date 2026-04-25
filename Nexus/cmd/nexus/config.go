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

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/term"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/crypto"
)

// runConfig dispatches `nexus config <subcommand>`.
func runConfig(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nexus config <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  set-password   set or change the master encryption password")
		fmt.Fprintln(os.Stderr, "  encrypt        encrypt sensitive fields in daemon.toml and source/destination TOML files")
		fmt.Fprintln(os.Stderr, "  decrypt        decrypt ENC:v1: fields in config TOML files (requires NEXUS_PASSWORD)")
		fmt.Fprintln(os.Stderr, "  show-secrets   print plaintext values of sensitive config fields")
		os.Exit(1)
	}

	switch args[0] {
	case "set-password":
		runConfigSetPassword()
	case "encrypt":
		runConfigEncrypt()
	case "decrypt":
		runConfigDecrypt()
	case "show-secrets":
		runConfigShowSecrets()
	default:
		fmt.Fprintf(os.Stderr, "nexus config: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// runConfigSetPassword implements `nexus config set-password` (also `nexus set-password`).
// It prompts the user for a password (with confirmation), retrying on mismatch,
// derives a master key, and stores the Argon2id salt at the canonical salt path.
func runConfigSetPassword() {
	saltPath, err := defaultSaltPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-password: cannot resolve salt path: %v\n", err)
		os.Exit(1)
	}

	const maxAttempts = 3
	var password string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Print("Enter encryption password: ")
		pw1, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "set-password: read password: %v\n", err)
			os.Exit(1)
		}

		if len(pw1) == 0 {
			fmt.Fprintln(os.Stderr, "  Password must not be empty. Try again.")
			continue
		}

		fmt.Print("Confirm password: ")
		pw2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "set-password: read confirmation: %v\n", err)
			os.Exit(1)
		}

		if string(pw1) != string(pw2) {
			remaining := maxAttempts - attempt
			if remaining > 0 {
				fmt.Fprintf(os.Stderr, "  Passwords do not match. %d attempt(s) remaining.\n\n", remaining)
				continue
			}
			fmt.Fprintln(os.Stderr, "  Passwords do not match. No attempts remaining.")
			os.Exit(1)
		}

		password = string(pw1)
		break
	}

	if password == "" {
		fmt.Fprintln(os.Stderr, "set-password: no valid password entered")
		os.Exit(1)
	}

	// Remove any existing salt so a fresh one is generated.
	if err := os.Remove(saltPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "set-password: remove old salt: %v\n", err)
		os.Exit(1)
	}

	mgr, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-password: derive master key: %v\n", err)
		os.Exit(1)
	}
	if !mgr.IsEnabled() {
		fmt.Fprintln(os.Stderr, "set-password: key derivation failed unexpectedly")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("✓ Encryption password set. Salt stored at: %s\n", saltPath)
	fmt.Println()
	fmt.Println("To enable encryption, start the daemon with:")
	fmt.Println("  $env:NEXUS_PASSWORD = \"<your-password>\"")
	fmt.Println("  nexus start")
}

// defaultSaltPath returns the canonical salt file path (~/.nexus/crypto.salt).
func defaultSaltPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".nexus", "crypto.salt"), nil
}

// runConfigEncrypt implements `nexus config encrypt`.
// It encrypts all sensitive plaintext fields in daemon.toml, sources/*.toml,
// and destinations/*.toml using the config sub-key derived from NEXUS_PASSWORD.
func runConfigEncrypt() {
	mkm := requireMKM("config encrypt")
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config encrypt: resolve config dir: %v\n", err)
		os.Exit(1)
	}

	configKey := mkm.SubKey("nexus-config-key-v1")
	total := 0

	files := collectConfigTOML(configDir)
	for _, path := range files {
		n, err := encryptTOMLFile(path, configKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config encrypt: %s: %v\n", path, err)
			os.Exit(1)
		}
		if n > 0 {
			fmt.Printf("config encrypt: %s — encrypted %d field(s)\n", path, n)
		}
		total += n
	}
	fmt.Printf("config encrypt: done — %d field(s) encrypted across %d file(s)\n", total, len(files))
}

// runConfigDecrypt implements `nexus config decrypt`.
// It decrypts all ENC:v1: fields in config TOML files.
func runConfigDecrypt() {
	mkm := requireMKM("config decrypt")
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config decrypt: resolve config dir: %v\n", err)
		os.Exit(1)
	}

	configKey := mkm.SubKey("nexus-config-key-v1")
	total := 0

	files := collectConfigTOML(configDir)
	for _, path := range files {
		n, err := decryptTOMLFile(path, configKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config decrypt: %s: %v\n", path, err)
			os.Exit(1)
		}
		if n > 0 {
			fmt.Printf("config decrypt: %s — decrypted %d field(s)\n", path, n)
		}
		total += n
	}
	fmt.Printf("config decrypt: done — %d field(s) decrypted across %d file(s)\n", total, len(files))
}

// runConfigShowSecrets implements `nexus config show-secrets`.
// It prints the plaintext value of every sensitive field in config TOML files.
func runConfigShowSecrets() {
	mkm := requireMKM("config show-secrets")
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config show-secrets: resolve config dir: %v\n", err)
		os.Exit(1)
	}

	configKey := mkm.SubKey("nexus-config-key-v1")

	files := collectConfigTOML(configDir)
	for _, path := range files {
		if err := showSecretsInFile(path, configKey); err != nil {
			fmt.Fprintf(os.Stderr, "config show-secrets: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

// requireMKM creates a MasterKeyManager from NEXUS_PASSWORD env.
// Exits with an error if no password is configured.
func requireMKM(cmd string) *crypto.MasterKeyManager {
	saltPath, err := defaultSaltPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: resolve salt path: %v\n", cmd, err)
		os.Exit(1)
	}
	mkm, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: derive master key: %v\n", cmd, err)
		os.Exit(1)
	}
	if !mkm.IsEnabled() {
		fmt.Fprintf(os.Stderr, "%s: no encryption password configured — set NEXUS_PASSWORD or run 'config set-password'\n", cmd)
		os.Exit(1)
	}
	return mkm
}

// collectConfigTOML returns the paths of all config TOML files:
// daemon.toml, sources/*.toml, destinations/*.toml.
func collectConfigTOML(configDir string) []string {
	var paths []string

	daemon := filepath.Join(configDir, "daemon.toml")
	if _, err := os.Stat(daemon); err == nil {
		paths = append(paths, daemon)
	}

	for _, sub := range []string{"sources", "destinations"} {
		pattern := filepath.Join(configDir, sub, "*.toml")
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	return paths
}

// tomlSensitiveRe matches TOML assignment lines with double-quoted string values:
//
//	(\s*)([a-zA-Z][a-zA-Z0-9_]*)\s*=\s*"([^"]*)"(.*)
//
// Groups: leading whitespace, field name, value, trailing content (comment).
var tomlSensitiveRe = regexp.MustCompile(`^(\s*)([a-zA-Z][a-zA-Z0-9_]*)\s*=\s*"([^"]*)"(.*)$`)

// encryptTOMLFile encrypts sensitive plaintext fields in the file at path.
// Returns the count of fields encrypted.
func encryptTOMLFile(path string, key [32]byte) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read: %w", err)
	}

	var buf bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		m := tomlSensitiveRe.FindStringSubmatch(line)
		if m != nil {
			indent, fieldName, value, trailing := m[1], m[2], m[3], m[4]
			if crypto.IsSensitiveFieldName(fieldName) && value != "" && !crypto.IsEncrypted(value) {
				enc, err := crypto.EncryptField(value, key)
				if err != nil {
					return count, fmt.Errorf("encrypt field %q: %w", fieldName, err)
				}
				line = indent + fieldName + ` = "` + enc + `"` + trailing
				count++
			}
		}
		buf.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan: %w", err)
	}

	// Write atomically: write to temp file then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return count, fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return count, fmt.Errorf("rename: %w", err)
	}
	return count, nil
}

// decryptTOMLFile decrypts ENC:v1: fields in the file at path.
// Returns the count of fields decrypted.
func decryptTOMLFile(path string, key [32]byte) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read: %w", err)
	}

	var buf bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		m := tomlSensitiveRe.FindStringSubmatch(line)
		if m != nil {
			indent, fieldName, value, trailing := m[1], m[2], m[3], m[4]
			if crypto.IsSensitiveFieldName(fieldName) && crypto.IsEncrypted(value) {
				plain, err := crypto.DecryptField(value, key)
				if err != nil {
					return count, fmt.Errorf("decrypt field %q: %w", fieldName, err)
				}
				line = indent + fieldName + ` = "` + plain + `"` + trailing
				count++
			}
		}
		buf.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return count, fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return count, fmt.Errorf("rename: %w", err)
	}
	return count, nil
}

// showSecretsInFile prints the plaintext value of every sensitive field in path.
// ENC:v1: values are decrypted before printing.
func showSecretsInFile(path string, key [32]byte) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	fmt.Printf("--- %s ---\n", path)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		m := tomlSensitiveRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fieldName, value := m[2], m[3]
		if !crypto.IsSensitiveFieldName(fieldName) || value == "" {
			continue
		}
		plaintext := value
		encrypted := crypto.IsEncrypted(value)
		if encrypted {
			plaintext, err = crypto.DecryptField(value, key)
			if err != nil {
				return fmt.Errorf("decrypt field %q: %w", fieldName, err)
			}
		}
		status := "plaintext"
		if encrypted {
			status = "encrypted"
		}
		fmt.Printf("  %-30s [%s]: %s\n", fieldName, status, plaintext)
	}
	return scanner.Err()
}

