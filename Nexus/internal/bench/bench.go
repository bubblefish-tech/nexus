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

// Package bench implements the `bubblefish bench` command: throughput, latency,
// and retrieval-evaluation benchmarks against a running Nexus daemon.
//
// All benchmarks are HTTP-client-based (not in-process) as required by
// Tech Spec Section 13.4.
package bench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

// Options configures a benchmark run.
type Options struct {
	// Mode is one of "throughput", "latency", or "eval".
	Mode string
	// URL is the base URL of the running Nexus daemon (e.g. http://127.0.0.1:8000).
	URL string
	// N is the number of requests to issue.
	N int
	// Concurrency is the number of concurrent workers (throughput mode only).
	Concurrency int
	// Source is the source name for write requests.
	Source string
	// Destination is the destination name for read requests.
	Destination string
	// APIKey is the data-plane API key.
	APIKey string
	// AdminKey is the admin token (needed for debug_stages in latency mode).
	AdminKey string
	// GoldenFile is the path to a known-good JSON for eval mode.
	GoldenFile string
	// Query is the search query string for latency and eval modes.
	Query string
	// OutputFile is the optional path for machine-readable JSON results.
	OutputFile string
	// Logger for structured output.
	Logger *slog.Logger
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// ThroughputResult holds the outcome of a throughput benchmark.
type ThroughputResult struct {
	Mode        string  `json:"mode"`
	Requests    int     `json:"requests"`
	Concurrency int     `json:"concurrency"`
	DurationMs  float64 `json:"duration_ms"`
	ReqPerSec   float64 `json:"req_per_sec"`
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	Errors      int     `json:"errors"`
}

// LatencyResult holds the outcome of a latency benchmark.
type LatencyResult struct {
	Mode               string             `json:"mode"`
	Requests           int                `json:"requests"`
	DurationMs         float64            `json:"duration_ms"`
	P50Ms              float64            `json:"p50_ms"`
	P95Ms              float64            `json:"p95_ms"`
	P99Ms              float64            `json:"p99_ms"`
	PerStageLatencyMs  map[string]float64 `json:"per_stage_latency_ms"`
	StagesHit          []string           `json:"stages_hit"`
	CacheHitRatio      float64            `json:"cache_hit_ratio"`
	Errors             int                `json:"errors"`
}

// EvalResult holds the outcome of an eval benchmark.
type EvalResult struct {
	Mode      string  `json:"mode"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	MRR       float64 `json:"mrr"`
	NDCG      float64 `json:"ndcg"`
	Expected  int     `json:"expected"`
	Retrieved int     `json:"retrieved"`
	Relevant  int     `json:"relevant"`
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

// Run executes the benchmark specified by opts and returns the JSON-serializable
// result. It writes machine-readable output to opts.OutputFile if set.
func Run(opts Options) (interface{}, error) {
	var result interface{}
	var err error

	switch opts.Mode {
	case "throughput":
		result, err = runThroughput(opts)
	case "latency":
		result, err = runLatency(opts)
	case "eval":
		result, err = runEval(opts)
	default:
		return nil, fmt.Errorf("unknown mode %q (expected throughput, latency, or eval)", opts.Mode)
	}
	if err != nil {
		return nil, err
	}

	if opts.OutputFile != "" {
		if writeErr := writeJSON(opts.OutputFile, result); writeErr != nil {
			return result, fmt.Errorf("write output: %w", writeErr)
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Throughput mode
// ---------------------------------------------------------------------------

func runThroughput(opts Options) (*ThroughputResult, error) {
	if opts.N <= 0 {
		opts.N = 100
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}

	client := &http.Client{Timeout: 30 * time.Second}
	latencies := make([]float64, 0, opts.N)
	var mu sync.Mutex
	var errCount atomic.Int64

	work := make(chan int, opts.N)
	for i := 0; i < opts.N; i++ {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < opts.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				dur, err := sendWrite(client, opts, i)
				if err != nil {
					errCount.Add(1)
					opts.Logger.Warn("bench: write failed", "error", err, "index", i)
					continue
				}
				mu.Lock()
				latencies = append(latencies, dur)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	sort.Float64s(latencies)

	result := &ThroughputResult{
		Mode:        "throughput",
		Requests:    opts.N,
		Concurrency: opts.Concurrency,
		DurationMs:  float64(elapsed.Milliseconds()),
		ReqPerSec:   float64(opts.N) / elapsed.Seconds(),
		P50Ms:       Percentile(latencies, 50),
		P95Ms:       Percentile(latencies, 95),
		P99Ms:       Percentile(latencies, 99),
		Errors:      int(errCount.Load()),
	}

	opts.Logger.Info("bench: throughput complete",
		"req_per_sec", fmt.Sprintf("%.1f", result.ReqPerSec),
		"p50_ms", fmt.Sprintf("%.1f", result.P50Ms),
		"p95_ms", fmt.Sprintf("%.1f", result.P95Ms),
		"p99_ms", fmt.Sprintf("%.1f", result.P99Ms),
		"errors", result.Errors,
	)

	return result, nil
}

func sendWrite(client *http.Client, opts Options, index int) (float64, error) {
	payload := map[string]string{
		"content":    fmt.Sprintf("bench-throughput-payload-%d", index),
		"collection": "bench",
		"subject":    "bench-test",
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/inbound/%s", opts.URL, opts.Source)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", opts.APIKey)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("bench-throughput-%d-%d", time.Now().UnixNano(), index))

	t0 := time.Now()
	resp, err := client.Do(req)
	dur := float64(time.Since(t0).Microseconds()) / 1000.0 // ms
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return dur, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return dur, nil
}

// ---------------------------------------------------------------------------
// Latency mode
// ---------------------------------------------------------------------------

func runLatency(opts Options) (*LatencyResult, error) {
	if opts.N <= 0 {
		opts.N = 50
	}
	if opts.Query == "" {
		opts.Query = "bench"
	}

	client := &http.Client{Timeout: 30 * time.Second}
	latencies := make([]float64, 0, opts.N)
	var errCount int

	// Aggregate per-stage latencies across all runs.
	stageLatencySum := make(map[string]float64)
	stageLatencyCount := make(map[string]int)
	stagesHitSet := make(map[string]struct{})
	var cacheHits int

	start := time.Now()

	for i := 0; i < opts.N; i++ {
		dur, debug, err := sendQuery(client, opts)
		if err != nil {
			errCount++
			opts.Logger.Warn("bench: query failed", "error", err, "index", i)
			continue
		}
		latencies = append(latencies, dur)

		if debug != nil {
			for stage, ms := range debug.PerStageLatencyMs {
				stageLatencySum[stage] += ms
				stageLatencyCount[stage]++
			}
			for _, s := range debug.StagesHit {
				stagesHitSet[s] = struct{}{}
			}
			if debug.CacheHit {
				cacheHits++
			}
		}
	}

	elapsed := time.Since(start)
	sort.Float64s(latencies)

	// Average per-stage latencies.
	avgStageLatency := make(map[string]float64, len(stageLatencySum))
	for stage, total := range stageLatencySum {
		avgStageLatency[stage] = total / float64(stageLatencyCount[stage])
	}

	stages := make([]string, 0, len(stagesHitSet))
	for s := range stagesHitSet {
		stages = append(stages, s)
	}
	sort.Strings(stages)

	successful := len(latencies)
	var cacheRatio float64
	if successful > 0 {
		cacheRatio = float64(cacheHits) / float64(successful)
	}

	result := &LatencyResult{
		Mode:              "latency",
		Requests:          opts.N,
		DurationMs:        float64(elapsed.Milliseconds()),
		P50Ms:             Percentile(latencies, 50),
		P95Ms:             Percentile(latencies, 95),
		P99Ms:             Percentile(latencies, 99),
		PerStageLatencyMs: avgStageLatency,
		StagesHit:         stages,
		CacheHitRatio:     cacheRatio,
		Errors:            errCount,
	}

	opts.Logger.Info("bench: latency complete",
		"p50_ms", fmt.Sprintf("%.1f", result.P50Ms),
		"p95_ms", fmt.Sprintf("%.1f", result.P95Ms),
		"p99_ms", fmt.Sprintf("%.1f", result.P99Ms),
		"stages_hit", stages,
		"cache_hit_ratio", fmt.Sprintf("%.2f", cacheRatio),
		"errors", result.Errors,
	)

	return result, nil
}

// debugPayload is the subset of the _nexus.debug response we need.
type debugPayload struct {
	StagesHit         []string           `json:"stages_hit"`
	PerStageLatencyMs map[string]float64 `json:"per_stage_latency_ms"`
	CacheHit          bool               `json:"cache_hit"`
	TotalLatencyMs    float64            `json:"total_latency_ms"`
}

type queryRespEnvelope struct {
	Nexus struct {
		Debug *debugPayload `json:"debug,omitempty"`
	} `json:"_nexus"`
}

func sendQuery(client *http.Client, opts Options) (float64, *debugPayload, error) {
	url := fmt.Sprintf("%s/query/%s?q=%s&debug_stages=true", opts.URL, opts.Destination, opts.Query)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	// Use admin key for debug_stages access.
	if opts.AdminKey != "" {
		req.Header.Set("X-API-Key", opts.AdminKey)
	} else {
		req.Header.Set("X-API-Key", opts.APIKey)
	}

	t0 := time.Now()
	resp, err := client.Do(req)
	dur := float64(time.Since(t0).Microseconds()) / 1000.0
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return dur, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var envelope queryRespEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return dur, nil, nil // still count latency even if parse fails
	}

	return dur, envelope.Nexus.Debug, nil
}

// ---------------------------------------------------------------------------
// Eval mode
// ---------------------------------------------------------------------------

// goldenEntry represents one expected retrieval result in the golden file.
type goldenEntry struct {
	PayloadID string `json:"payload_id"`
	Content   string `json:"content"`
}

// goldenFile is the top-level structure of the known-good JSON file.
type goldenFile struct {
	Query    string        `json:"query"`
	Expected []goldenEntry `json:"expected"`
}

type evalQueryResult struct {
	PayloadID string `json:"payload_id"`
	Content   string `json:"content"`
}

type evalQueryResp struct {
	Results []evalQueryResult `json:"results"`
}

func runEval(opts Options) (*EvalResult, error) {
	if opts.GoldenFile == "" {
		return nil, fmt.Errorf("eval mode requires --golden file path")
	}

	raw, err := os.ReadFile(opts.GoldenFile)
	if err != nil {
		return nil, fmt.Errorf("read golden file: %w", err)
	}

	var golden goldenFile
	if err := json.Unmarshal(raw, &golden); err != nil {
		return nil, fmt.Errorf("parse golden file: %w", err)
	}

	if golden.Query == "" {
		return nil, fmt.Errorf("golden file missing 'query' field")
	}
	if len(golden.Expected) == 0 {
		return nil, fmt.Errorf("golden file has empty 'expected' array")
	}

	// Issue the query against the live daemon.
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("%s/query/%s?q=%s&limit=%d", opts.URL, opts.Destination, golden.Query, len(golden.Expected)*2)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", opts.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query daemon: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var qr evalQueryResp
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("parse query response: %w", err)
	}

	// Build expected set.
	expectedSet := make(map[string]bool, len(golden.Expected))
	for _, e := range golden.Expected {
		expectedSet[e.PayloadID] = true
	}

	// Build ranked list of retrieved payload IDs.
	retrieved := make([]string, len(qr.Results))
	for i, r := range qr.Results {
		retrieved[i] = r.PayloadID
	}

	// Build expected ordered list for NDCG.
	expectedOrder := make([]string, len(golden.Expected))
	for i, e := range golden.Expected {
		expectedOrder[i] = e.PayloadID
	}

	precision, recall, relevant := PrecisionRecall(retrieved, expectedSet)
	mrr := MRR(retrieved, expectedSet)
	ndcg := NDCG(retrieved, expectedOrder)

	result := &EvalResult{
		Mode:      "eval",
		Precision: precision,
		Recall:    recall,
		MRR:       mrr,
		NDCG:      ndcg,
		Expected:  len(golden.Expected),
		Retrieved: len(retrieved),
		Relevant:  relevant,
	}

	opts.Logger.Info("bench: eval complete",
		"precision", fmt.Sprintf("%.3f", precision),
		"recall", fmt.Sprintf("%.3f", recall),
		"mrr", fmt.Sprintf("%.3f", mrr),
		"ndcg", fmt.Sprintf("%.3f", ndcg),
	)

	return result, nil
}

// ---------------------------------------------------------------------------
// Metrics helpers (exported for testing)
// ---------------------------------------------------------------------------

// Percentile computes the p-th percentile from a sorted slice of float64
// values. Returns 0 if the slice is empty. p must be in [0, 100].
func Percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[n-1]
	}
	rank := (p / 100) * float64(n-1)
	lower := int(rank)
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// PrecisionRecall computes precision and recall given a list of retrieved IDs
// and a set of expected (relevant) IDs. Returns (precision, recall, relevant_count).
func PrecisionRecall(retrieved []string, expected map[string]bool) (float64, float64, int) {
	if len(retrieved) == 0 && len(expected) == 0 {
		return 1.0, 1.0, 0
	}
	if len(retrieved) == 0 {
		return 0, 0, 0
	}

	relevant := 0
	for _, id := range retrieved {
		if expected[id] {
			relevant++
		}
	}

	precision := float64(relevant) / float64(len(retrieved))
	recall := 0.0
	if len(expected) > 0 {
		recall = float64(relevant) / float64(len(expected))
	}
	return precision, recall, relevant
}

// MRR computes Mean Reciprocal Rank. It returns 1/(rank of first relevant result)
// or 0 if no relevant results are found.
func MRR(retrieved []string, expected map[string]bool) float64 {
	for i, id := range retrieved {
		if expected[id] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCG computes Normalized Discounted Cumulative Gain. expectedOrder is the
// ideal ranking. retrieved is the actual ranking from the system.
func NDCG(retrieved []string, expectedOrder []string) float64 {
	if len(expectedOrder) == 0 {
		return 0
	}

	// Build relevance map: position in ideal order → relevance score.
	// Use 1-based relevance: the first item in expectedOrder gets the highest score.
	relevance := make(map[string]float64, len(expectedOrder))
	for i, id := range expectedOrder {
		relevance[id] = float64(len(expectedOrder) - i)
	}

	// DCG of retrieved list.
	dcg := 0.0
	k := len(retrieved)
	if k > len(expectedOrder) {
		k = len(expectedOrder)
	}
	for i := 0; i < len(retrieved) && i < k; i++ {
		rel := relevance[retrieved[i]]
		dcg += rel / math.Log2(float64(i+2)) // i+2 because log2(1)=0
	}

	// Ideal DCG (expectedOrder in its natural order).
	idcg := 0.0
	for i := 0; i < k; i++ {
		rel := float64(len(expectedOrder) - i)
		idcg += rel / math.Log2(float64(i+2))
	}

	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
