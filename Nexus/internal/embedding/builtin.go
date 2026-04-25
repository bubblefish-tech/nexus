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

package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// BuiltinProvider manages a llama-server subprocess for local embedding inference.
// It speaks the OpenAI-compatible /v1/embeddings API on a random localhost port.
// Zero external dependencies — user never needs to install Ollama or any other service.
type BuiltinProvider struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	port       int
	modelPath  string
	serverPath string
	baseURL    string
	client     *http.Client
	logger     *slog.Logger
	dimensions int
	started    bool
	stopped    chan struct{}
}

// BuiltinConfig holds configuration for the builtin embedding provider.
type BuiltinConfig struct {
	ModelPath   string
	ServerPath  string
	Dimensions  int
	ContextSize int
	BatchSize   int
	GPULayers   int
	Logger      *slog.Logger
}

// DefaultBuiltinConfig returns safe defaults for nomic-embed-text-v1.5.
func DefaultBuiltinConfig(configDir string) BuiltinConfig {
	serverName := "llama-server"
	if runtime.GOOS == "windows" {
		serverName = "llama-server.exe"
	}
	return BuiltinConfig{
		ModelPath:   filepath.Join(configDir, "models", "nomic-embed-text-v1.5.Q4_K_S.gguf"),
		ServerPath:  filepath.Join(configDir, "models", serverName),
		Dimensions:  768,
		ContextSize: 2048,
		BatchSize:   2048,
		GPULayers:   0,
		Logger:      slog.Default(),
	}
}

// NewBuiltinProvider creates a provider but does NOT start the subprocess yet.
// Call Start() to launch llama-server.
func NewBuiltinProvider(cfg BuiltinConfig) (*BuiltinProvider, error) {
	if _, err := os.Stat(cfg.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("builtin embedding model not found: %s (run 'nexus install' or scripts/fetch-embedding-model)", cfg.ModelPath)
	}
	if _, err := os.Stat(cfg.ServerPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("llama-server binary not found: %s (run 'nexus install' or scripts/fetch-embedding-model)", cfg.ServerPath)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &BuiltinProvider{
		modelPath:  cfg.ModelPath,
		serverPath: cfg.ServerPath,
		dimensions: cfg.Dimensions,
		logger:     logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:    90 * time.Second,
			},
		},
		stopped: make(chan struct{}),
	}, nil
}

// Start launches the llama-server subprocess. Blocks until the server is healthy.
func (p *BuiltinProvider) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}

	return p.startProcessLocked(ctx)
}

func (p *BuiltinProvider) startProcessLocked(ctx context.Context) error {
	port, err := freePort()
	if err != nil {
		return fmt.Errorf("find free port: %w", err)
	}
	p.port = port
	p.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	ctxSize := "2048"
	batchSize := "2048"
	gpuLayers := "0"

	args := []string{
		"--model", p.modelPath,
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1",
		"--embedding",
		"--ctx-size", ctxSize,
		"--batch-size", batchSize,
		"--n-gpu-layers", gpuLayers,
		"--log-disable",
		"--rope-scaling", "yarn",
		"--rope-freq-scale", "0.75",
	}

	// Use background context for the process lifetime — the caller's ctx
	// is only for the startup health-check timeout, not the process lifespan.
	p.cmd = exec.Command(p.serverPath, args...)
	p.cmd.Stdout = io.Discard
	p.cmd.Stderr = io.Discard

	p.logger.Info("builtin embedding: starting llama-server",
		"port", port,
		"model", filepath.Base(p.modelPath),
	)

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}

	p.logger.Info("builtin embedding: llama-server started",
		"port", port,
		"pid", p.cmd.Process.Pid,
	)

	healthy := false
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := p.client.Get(p.baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				healthy = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !healthy {
		p.cmd.Process.Kill()
		return fmt.Errorf("llama-server did not become healthy within 60s on port %d", port)
	}

	p.started = true
	p.logger.Info("builtin embedding: server healthy",
		"port", port,
		"model", filepath.Base(p.modelPath),
	)

	go p.watchProcess()

	return nil
}

func (p *BuiltinProvider) watchProcess() {
	restarts := 0
	backoff := 2 * time.Second

	for {
		err := p.cmd.Wait()

		select {
		case <-p.stopped:
			return
		default:
		}

		restarts++
		p.mu.Lock()
		p.started = false
		p.mu.Unlock()

		if restarts > 3 {
			p.logger.Error("builtin embedding: llama-server crashed too many times, giving up",
				"restarts", restarts,
				"last_error", err,
			)
			return
		}

		p.logger.Warn("builtin embedding: llama-server exited, restarting",
			"error", err,
			"restart_count", restarts,
			"backoff", backoff,
		)

		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}

		p.mu.Lock()
		startErr := p.startProcessLocked(context.Background())
		p.mu.Unlock()
		if startErr != nil {
			p.logger.Error("builtin embedding: restart failed", "error", startErr)
			continue
		}
		restarts = 0
		backoff = 2 * time.Second
	}
}

// Embed returns the embedding vector for a single text.
func (p *BuiltinProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if !started {
		return nil, ErrEmbeddingUnavailable
	}

	prefixed := "search_query: " + text

	reqBody, err := json.Marshal(map[string]interface{}{
		"input": prefixed,
		"model": "nomic-embed-text-v1.5",
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", ErrEmbeddingUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("%w: request: %v", ErrEmbeddingUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrEmbeddingUnavailable, resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrEmbeddingUnavailable, err)
	}

	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("%w: empty embedding in response", ErrEmbeddingUnavailable)
	}

	return result.Data[0].Embedding, nil
}

// BatchEmbed returns vectors for multiple texts.
func (p *BuiltinProvider) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = "search_query: " + t
	}

	reqBody, err := json.Marshal(map[string]interface{}{
		"input": prefixed,
		"model": "nomic-embed-text-v1.5",
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", ErrEmbeddingUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("%w: request: %v", ErrEmbeddingUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrEmbeddingUnavailable, resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrEmbeddingUnavailable, err)
	}

	vectors := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

// Dimensions returns the embedding vector dimension count.
func (p *BuiltinProvider) Dimensions() int {
	return p.dimensions
}

// Close stops the llama-server subprocess.
func (p *BuiltinProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.stopped:
		return nil
	default:
		close(p.stopped)
	}

	if p.cmd != nil && p.cmd.Process != nil {
		p.logger.Info("builtin embedding: stopping llama-server",
			"pid", p.cmd.Process.Pid,
		)
		p.cmd.Process.Signal(os.Interrupt)

		done := make(chan struct{})
		go func() {
			p.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			p.cmd.Process.Kill()
		}
	}

	p.started = false
	return nil
}

// IsHealthy returns true if llama-server is responding.
func (p *BuiltinProvider) IsHealthy() bool {
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if !started {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/health", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// HealthStatus returns status info for the daemon /health endpoint.
func (p *BuiltinProvider) HealthStatus() (string, map[string]interface{}) {
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if !started {
		return "stopped", map[string]interface{}{"pid": 0}
	}
	healthy := p.IsHealthy()
	status := "ok"
	if !healthy {
		status = "degraded"
	}
	return status, map[string]interface{}{
		"pid":   p.cmd.Process.Pid,
		"port":  p.port,
		"model": filepath.Base(p.modelPath),
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

var _ EmbeddingClient = (*BuiltinProvider)(nil)
