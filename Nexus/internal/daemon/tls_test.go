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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
)

// generateTestCert creates a self-signed certificate and key for testing.
// Returns paths to the cert and key PEM files in a temporary directory.
func generateTestCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}
	if err := certFile.Close(); err != nil {
		t.Fatalf("close cert: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("encode key: %v", err)
	}
	if err := keyFile.Close(); err != nil {
		t.Fatalf("close key: %v", err)
	}

	return certPath, keyPath
}

// generateTestCA creates a self-signed CA certificate for mTLS testing.
// Returns the path to the CA cert PEM file.
func generateTestCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	f, err := os.Create(caPath)
	if err != nil {
		t.Fatalf("create CA file: %v", err)
	}
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("encode CA cert: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close CA cert file: %v", err)
	}

	return caPath
}

// literalResolve is a test resolver that returns values as-is (literal).
func literalResolve(s string) (string, error) {
	return s, nil
}

func TestBuildTLSConfig_Disabled(t *testing.T) {
	cfg := config.TLSConfig{Enabled: false}
	got, err := buildTLSConfig(cfg, literalResolve)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil TLS config when disabled")
	}
}

func TestBuildTLSConfig_ValidCerts(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	cfg := config.TLSConfig{
		Enabled:    true,
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: "1.2",
		MaxVersion: "1.3",
	}

	got, err := buildTLSConfig(cfg, literalResolve)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil TLS config")
	}
	if got.MinVersion != tls.VersionTLS12 {
		t.Errorf("min version = %d; want %d", got.MinVersion, tls.VersionTLS12)
	}
	if got.MaxVersion != tls.VersionTLS13 {
		t.Errorf("max version = %d; want %d", got.MaxVersion, tls.VersionTLS13)
	}
	if len(got.Certificates) != 1 {
		t.Errorf("certificates count = %d; want 1", len(got.Certificates))
	}
}

func TestBuildTLSConfig_MissingCertFile(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled:  true,
		CertFile: "",
		KeyFile:  "/some/key.pem",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error for missing cert_file")
	}
}

func TestBuildTLSConfig_MissingKeyFile(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled:  true,
		CertFile: "/some/cert.pem",
		KeyFile:  "",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error for missing key_file")
	}
}

func TestBuildTLSConfig_UnreadableCert(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error for unreadable cert")
	}
}

func TestBuildTLSConfig_InvalidMinVersion(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	cfg := config.TLSConfig{
		Enabled:    true,
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: "99.9",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error for invalid min_version")
	}
}

func TestBuildTLSConfig_MinGreaterThanMax(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	cfg := config.TLSConfig{
		Enabled:    true,
		CertFile:   certPath,
		KeyFile:    keyPath,
		MinVersion: "1.3",
		MaxVersion: "1.2",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error when min_version > max_version")
	}
}

func TestBuildTLSConfig_mTLS_RequireAndVerify(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	caPath := generateTestCA(t)

	cfg := config.TLSConfig{
		Enabled:      true,
		CertFile:     certPath,
		KeyFile:      keyPath,
		ClientCAFile: caPath,
		ClientAuth:   "require_and_verify",
	}

	got, err := buildTLSConfig(cfg, literalResolve)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("client auth = %v; want RequireAndVerifyClientCert", got.ClientAuth)
	}
	if got.ClientCAs == nil {
		t.Fatal("expected non-nil ClientCAs pool")
	}
}

func TestBuildTLSConfig_mTLS_VerifyIfGiven(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	caPath := generateTestCA(t)

	cfg := config.TLSConfig{
		Enabled:      true,
		CertFile:     certPath,
		KeyFile:      keyPath,
		ClientCAFile: caPath,
		ClientAuth:   "verify_if_given",
	}

	got, err := buildTLSConfig(cfg, literalResolve)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("client auth = %v; want VerifyClientCertIfGiven", got.ClientAuth)
	}
}

func TestBuildTLSConfig_InvalidClientAuth(t *testing.T) {
	certPath, keyPath := generateTestCert(t)
	cfg := config.TLSConfig{
		Enabled:    true,
		CertFile:   certPath,
		KeyFile:    keyPath,
		ClientAuth: "bogus",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error for invalid client_auth mode")
	}
}

func TestBuildTLSConfig_InvalidCAFile(t *testing.T) {
	certPath, keyPath := generateTestCert(t)

	// Write a file with invalid PEM content.
	dir := t.TempDir()
	badCA := filepath.Join(dir, "bad-ca.pem")
	if err := os.WriteFile(badCA, []byte("not a cert"), 0600); err != nil {
		t.Fatalf("write bad CA: %v", err)
	}

	cfg := config.TLSConfig{
		Enabled:      true,
		CertFile:     certPath,
		KeyFile:      keyPath,
		ClientCAFile: badCA,
		ClientAuth:   "require_and_verify",
	}
	_, err := buildTLSConfig(cfg, literalResolve)
	if err == nil {
		t.Fatal("expected error for invalid CA PEM")
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		input   string
		def     uint16
		want    uint16
		wantErr bool
	}{
		{"", tls.VersionTLS12, tls.VersionTLS12, false},
		{"1.0", 0, tls.VersionTLS10, false},
		{"1.1", 0, tls.VersionTLS11, false},
		{"1.2", 0, tls.VersionTLS12, false},
		{"1.3", 0, tls.VersionTLS13, false},
		{"2.0", 0, 0, true},
		{"invalid", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseTLSVersion(tt.input, tt.def)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTLSVersion(%q) error = %v; wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseTLSVersion(%q) = %d; want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseClientAuth(t *testing.T) {
	tests := []struct {
		input   string
		want    tls.ClientAuthType
		wantErr bool
	}{
		{"", tls.NoClientCert, false},
		{"no_client_cert", tls.NoClientCert, false},
		{"require_and_verify", tls.RequireAndVerifyClientCert, false},
		{"verify_if_given", tls.VerifyClientCertIfGiven, false},
		{"invalid_mode", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseClientAuth(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseClientAuth(%q) error = %v; wantErr = %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseClientAuth(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}
