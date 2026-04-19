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

package daemon

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// buildTLSConfig constructs a *tls.Config from the daemon TLS configuration.
// It loads the server certificate and key, optionally loads the client CA pool
// for mTLS, and sets the minimum and maximum TLS versions.
//
// Returns nil if TLS is not enabled.
//
// INVARIANT: If TLS is enabled but cert_file or key_file is missing or
// unreadable, this function returns an error and the daemon MUST refuse to
// start. Reference: Tech Spec Section 6.2.
func buildTLSConfig(cfg config.TLSConfig, resolve func(string) (string, error)) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Resolve cert and key file paths (supports env:/file:/literal references).
	certPath, err := resolve(cfg.CertFile)
	if err != nil {
		return nil, fmt.Errorf("tls: resolve cert_file: %w", err)
	}
	if certPath == "" {
		return nil, fmt.Errorf("tls: cert_file is empty but TLS is enabled")
	}

	keyPath, err := resolve(cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: resolve key_file: %w", err)
	}
	if keyPath == "" {
		return nil, fmt.Errorf("tls: key_file is empty but TLS is enabled")
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("tls: load certificate: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Set minimum TLS version.
	minVer, err := parseTLSVersion(cfg.MinVersion, tls.VersionTLS12)
	if err != nil {
		return nil, fmt.Errorf("tls: min_version: %w", err)
	}
	tlsCfg.MinVersion = minVer

	// Set maximum TLS version.
	maxVer, err := parseTLSVersion(cfg.MaxVersion, tls.VersionTLS13)
	if err != nil {
		return nil, fmt.Errorf("tls: max_version: %w", err)
	}
	tlsCfg.MaxVersion = maxVer

	if tlsCfg.MinVersion > tlsCfg.MaxVersion {
		return nil, fmt.Errorf("tls: min_version (%s) > max_version (%s)", cfg.MinVersion, cfg.MaxVersion)
	}

	// Configure client certificate authentication (mTLS).
	// Reference: Tech Spec Section 6.2.
	clientAuth, err := parseClientAuth(cfg.ClientAuth)
	if err != nil {
		return nil, fmt.Errorf("tls: client_auth: %w", err)
	}
	tlsCfg.ClientAuth = clientAuth

	// Load client CA pool if client_ca_file is set.
	if cfg.ClientCAFile != "" {
		caPath, err := resolve(cfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("tls: resolve client_ca_file: %w", err)
		}
		if caPath == "" {
			return nil, fmt.Errorf("tls: client_ca_file resolved to empty value")
		}
		caPEM, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("tls: read client_ca_file %q: %w", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("tls: client_ca_file %q contains no valid PEM certificates", caPath)
		}
		tlsCfg.ClientCAs = pool
	}

	return tlsCfg, nil
}

// parseTLSVersion converts a version string like "1.2" or "1.3" to the
// corresponding crypto/tls constant. Returns defaultVer if the string is empty.
func parseTLSVersion(s string, defaultVer uint16) (uint16, error) {
	if s == "" {
		return defaultVer, nil
	}
	switch s {
	case "1.0":
		return tls.VersionTLS10, nil
	case "1.1":
		return tls.VersionTLS11, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version %q (valid: 1.0, 1.1, 1.2, 1.3)", s)
	}
}

// parseClientAuth converts a client_auth string from the config to the
// corresponding crypto/tls constant.
//
// Supported values:
//   - "no_client_cert" (default)
//   - "require_and_verify"
//   - "verify_if_given"
//
// Reference: Tech Spec Section 6.2.
func parseClientAuth(s string) (tls.ClientAuthType, error) {
	if s == "" {
		return tls.NoClientCert, nil
	}
	switch s {
	case "no_client_cert":
		return tls.NoClientCert, nil
	case "require_and_verify":
		return tls.RequireAndVerifyClientCert, nil
	case "verify_if_given":
		return tls.VerifyClientCertIfGiven, nil
	default:
		return 0, fmt.Errorf("unsupported client_auth mode %q (valid: no_client_cert, require_and_verify, verify_if_given)", s)
	}
}
