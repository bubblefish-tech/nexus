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

// Package daemon implements the BubbleFish Nexus gateway daemon. It wires
// together the WAL, queue, idempotency store, destination adapter, HTTP server,
// authentication middleware, request handlers, Prometheus metrics, hot reload
// watcher, and 3-stage graceful shutdown.
//
// Lifecycle:
//
//	New()   — validates dependencies, wires components, initialises metrics
//	Start() — opens WAL and destination, starts HTTP server, runs forever
//	Stop()  — 3-stage budgeted shutdown: HTTP → queue drain → WAL close
//
// All state is held in struct fields. There are no package-level variables.
package daemon

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"database/sql"

	a2aclient "github.com/bubblefish-tech/nexus/internal/a2a/client"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	a2aserver "github.com/bubblefish-tech/nexus/internal/a2a/server"
	"github.com/bubblefish-tech/nexus/internal/actions"
	"github.com/bubblefish-tech/nexus/internal/agent"
	"github.com/bubblefish-tech/nexus/internal/approvals"
	"github.com/bubblefish-tech/nexus/internal/audit"
	"github.com/bubblefish-tech/nexus/internal/coordination"
	"github.com/bubblefish-tech/nexus/internal/credentials"
	"github.com/bubblefish-tech/nexus/internal/cache"
	"github.com/bubblefish-tech/nexus/internal/canonical"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/destination"
	destfactory "github.com/bubblefish-tech/nexus/internal/destination/factory"
	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/doctor"
	"github.com/bubblefish-tech/nexus/internal/embedding"
	"github.com/bubblefish-tech/nexus/internal/maintain"
	"github.com/bubblefish-tech/nexus/internal/eventbus"
	"github.com/bubblefish-tech/nexus/internal/events"
	"github.com/bubblefish-tech/nexus/internal/eventsink"
	"github.com/bubblefish-tech/nexus/internal/firewall"
	"github.com/bubblefish-tech/nexus/internal/grants"
	"github.com/bubblefish-tech/nexus/internal/hotreload"
	"github.com/bubblefish-tech/nexus/internal/idempotency"
	"github.com/bubblefish-tech/nexus/internal/immune"
	"github.com/bubblefish-tech/nexus/internal/jwtauth"
	"github.com/bubblefish-tech/nexus/internal/mcp"
	"github.com/bubblefish-tech/nexus/internal/metrics"
	"github.com/bubblefish-tech/nexus/internal/oauth"
	"github.com/bubblefish-tech/nexus/internal/orchestrate"
	"github.com/bubblefish-tech/nexus/internal/policy"
	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/quarantine"
	"github.com/bubblefish-tech/nexus/internal/queue"
	"github.com/bubblefish-tech/nexus/internal/secrets"
	"github.com/bubblefish-tech/nexus/internal/securitylog"
	"github.com/bubblefish-tech/nexus/internal/signing"
	"github.com/bubblefish-tech/nexus/internal/subscribe"
	"github.com/bubblefish-tech/nexus/internal/substrate"
	"github.com/bubblefish-tech/nexus/internal/supervisor"
	"github.com/bubblefish-tech/nexus/internal/tasks"
	"github.com/bubblefish-tech/nexus/internal/version"
	"github.com/bubblefish-tech/nexus/internal/vizpipe"
	"github.com/bubblefish-tech/nexus/internal/wal"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

// Daemon is the central BubbleFish Nexus gateway daemon. All state is held in
// struct fields; there are no package-level variables.
type Daemon struct {
	// configMu guards cfg. Auth hot path uses RLock(); hot reload uses Lock().
	// INVARIANT: NEVER call Lock() on the auth hot path. Only RLock().
	// Reference: Phase 0D Behavioral Contract items 5–6.
	configMu sync.RWMutex
	cfg      *config.Config

	logger  *slog.Logger
	metrics *metrics.Metrics

	wal             *wal.WAL
	queue           *queue.Queue
	idem            *idempotency.Store
	dest            destination.Destination
	querier         destination.Querier
	embeddingClient embedding.EmbeddingClient // nil when embedding disabled
	exactCache      *cache.ExactCache         // Stage 1 — Phase 4
	semanticCache   *cache.SemanticCache      // Stage 2 — Phase 6
	server          *http.Server
	rl              *rateLimiter
	bytesRL         *bytesRateLimiter

	reloadWatcher *hotreload.Watcher
	mcpServer     *mcp.Server // nil when MCP is disabled or failed to start

	// securityLog is the structured security event logger. Nil when
	// security_events.enabled is false. Reference: Tech Spec Section 11.2.
	securityLog *securitylog.Logger

	// eventSink is the webhook event sink. Nil when daemon.events.enabled
	// is false. Reference: Tech Spec Section 10.
	eventSink *eventsink.Sink

	// jwtValidator validates JWTs against a cached JWKS. Nil when
	// daemon.jwt.enabled is false. Reference: Tech Spec Section 6.6.
	jwtValidator *jwtauth.Validator

	// vizPipe is the live pipeline visualization event pipe. Always initialized
	// (never nil). Reference: Tech Spec Section 13.2.
	vizPipe *vizpipe.Pipe

	// trustedProxies holds parsed CIDR networks for determining effective
	// client IP from forwarded headers. Nil when no trusted proxies are
	// configured. Reference: Tech Spec Section 6.3.
	proxies *trustedProxies

	// signingKey holds the resolved signing key bytes when [daemon.signing]
	// enabled = true. Nil when signing is disabled. Used at startup and by
	// hot reload to verify compiled config signatures.
	// NEVER log this value.
	signingKey []byte

	// retrievalFirewall is the retrieval firewall engine. Nil when
	// [daemon.retrieval_firewall] enabled = false.
	// Reference: Tech Spec Addendum Section A3.1.
	retrievalFirewall *firewall.RetrievalFirewall

	// auditLogger is the interaction log writer (JSONL files). Nil when
	// [daemon.audit] enabled = false.
	// Reference: Tech Spec Addendum Section A2.3.
	auditLogger *audit.AuditLogger

	// auditWAL writes audit records to the WAL for kill-9 durability.
	// Nil when [daemon.audit] enabled = false.
	auditWAL *audit.WALWriter

	// auditReader reads and queries the interaction log. Nil when
	// [daemon.audit] enabled = false.
	// Reference: Tech Spec Addendum Section A2.5.
	auditReader *audit.AuditReader

	// auditRateLimiter rate-limits /api/audit/* queries per admin token.
	// Reference: Tech Spec Addendum Section A2.5.
	auditRateLimiter *rateLimiter

	// walHealthy tracks whether the WAL watchdog considers the WAL healthy.
	// 1 = healthy, 0 = unhealthy. Checked by /ready to return 503.
	// Reference: Tech Spec Section 4.4.
	walHealthy atomic.Int32

	// consistencyScore stores the latest WAL-to-destination consistency score
	// (0.0–1.0) as float64 bits. -1 means not yet computed. Read via
	// math.Float64frombits. Reference: Tech Spec Section 11.5.
	consistencyScore atomic.Uint64

	// startedAt records when Start() was called, for uptime calculation.
	startedAt time.Time

	// a2aServer is the NA2A JSON-RPC server exposed on POST /a2a/jsonrpc.
	// Nil until setupA2ABridge succeeds. Provides agent/register when a
	// registration token is configured.
	a2aServer *a2aserver.Server

	// a2aPool is the shared client connection pool for outbound A2A calls.
	// Nil until setupA2ABridge succeeds. Used by the admin register endpoint
	// to evict stale connections when a URL changes.
	a2aPool *a2aclient.Pool

	// registryStore holds the A2A agent registry (configDir/a2a/registry.db).
	// Opened unconditionally early in Start(); owns the shared *sql.DB used
	// by the control-plane stores below and — when [a2a] enabled — by the
	// A2A bridge. Nil until Start() opens it; if the open fails the daemon
	// logs a warning and proceeds without control/A2A features.
	registryStore *registry.Store

	// Control-plane stores (MT.1/MT.2). All four share registryStore.DB()
	// so grants/approvals/tasks/actions foreign-key directly against the
	// real a2a_agents table. Nil until Start() opens the registry and
	// cfg.Control.Enabled is true; routes in handlers_control.go register
	// only when grantStore is non-nil.
	grantStore    *grants.Store
	approvalStore *approvals.Store
	taskStore     *tasks.Store
	actionStore   *actions.Store

	// policyEngine is the MT.3 Nexus-native policy evaluation engine.
	// Nil when cfg.Control.Enabled is false or registry failed to open.
	policyEngine *policy.Engine

	// mkm is the master key manager derived from NEXUS_PASSWORD at startup.
	// Shared by memory encryption (CU.0.2) and control-plane encryption (CU.0.4).
	// Nil when home directory is unavailable or key derivation fails.
	mkm *nexuscrypto.MasterKeyManager

	// exactStats and semanticStats hold cache counter references for the
	// /api/status and /api/cache admin endpoints.
	exactStats    *cache.Stats
	semanticStats *cache.SemanticStats

	// supervisor monitors goroutine heartbeats and kills the process on stall.
	// Nil when not started.
	supervisor *supervisor.Supervisor

	// embeddingValidator validates embedding envelopes and tracks quarantine state.
	// Nil when embedding is disabled. Reference: v0.1.3 Build Plan Phase 2 Subtask 2.5.
	embeddingValidator *embeddingValidator

	// chainState maintains the hash-chained audit log. Nil when audit is disabled.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.3.
	chainState *provenance.ChainState

	// daemonKeyPair is the daemon's Ed25519 keypair for signing genesis entries,
	// Merkle roots, and query attestations. Nil when secrets dir unavailable.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
	daemonKeyPair *provenance.KeyPair

	// sourceKeys maps source name → loaded Ed25519 keypair for sources with
	// [source.signing] mode = "local". Nil entries mean signing disabled.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
	sourceKeys map[string]*provenance.KeyPair

	// sessionMgr tracks active agent sessions in memory. Always initialized.
	// Reference: AG.2.
	sessionMgr *agent.SessionManager

	// activityLog records agent activity events for telemetry and dashboard.
	// Always initialized. Reference: AG.7.
	activityLog *agent.ActivityLog

	// healthTracker monitors agent liveness for health state transitions.
	// Always initialized. Reference: AG.8.
	healthTracker *agent.HealthTracker

	// ── Agent Gateway subsystems (AG.1–AG.8) ────────────────────────────
	agentDB            *sql.DB                       // separate connection for agent registry
	agentRegistry      *agent.Registry               // AG.1
	credentialGateway  *credentials.Gateway           // AG.3
	toolPolicyChecker  *policy.ToolPolicyChecker      // AG.4
	signalQueue        *coordination.SignalQueue      // AG.5
	quotaManager       *agent.QuotaManager            // AG.6

	// ── BF-Sketch substrate (BS.1–BS.10) ────────────────────────────────
	// substrate is the BF-Sketch coordinator. Nil-safe when disabled.
	// Reference: v0.1.3 BF-Sketch Substrate Build Plan.
	substrate *substrate.Substrate

	// canonical is the embedding canonicalization pipeline. Nil-safe when disabled.
	// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2.
	canonical *canonical.Manager

	// immuneScanner runs Tier-0 heuristic rules on every inbound write.
	// Always initialized in Start(). Never nil after Start().
	// Reference: DEF.1, DEF.2.
	immuneScanner *immune.Scanner

	// quarantineStore persists Tier-0 intercepts. Nil when configDir is
	// unavailable. Gated routes and handlers check for nil before use.
	// Reference: DEF.2.
	quarantineStore *quarantine.Store

	// eventBus is the WebUI activity event bus (WEB.3). Always initialised in
	// New(); never nil. Publishes memory_written, memory_queried,
	// agent_connected, agent_disconnected, quarantine_event, ingest,
	// and discovery_event to SSE clients at GET /api/events/stream.
	eventBus *eventbus.Bus

	// liteBus is the Event Bus Lite (EVT.1). Single-consumer bus with richer
	// Data map[string]any payload. A bridge goroutine forwards events to
	// eventBus so all events appear in the SSE feed.
	liteBus *events.LiteBus

	// subscribeStore holds semantic subscriptions. Nil when the registry DB is
	// unavailable. Reference: SNC.2.
	subscribeStore   *subscribe.Store
	subscribeMatcher *subscribe.Matcher

	// pipeMetrics tracks per-stage latency and throughput for the TUI dashboard.
	pipeMetrics *pipelineMetrics

	// discoveryScanner runs the 5-tier AI-tool discovery scan on demand for
	// GET /api/discover/results. Nil until Start() resolves configDir.
	discoveryScanner *discover.Scanner

	// lastDiscoveryMu guards lastDiscovery and lastDiscoveryAt.
	lastDiscoveryMu sync.RWMutex
	lastDiscovery   []discover.DiscoveredTool
	lastDiscoveryAt time.Time

	stopOnce    sync.Once
	stopped     chan struct{}
	shutdownReq chan struct{} // closed by RequestShutdown; start.go selects on it
}

// New creates a Daemon from the loaded configuration. It does NOT open any
// files or start any goroutines — call Start() for that.
//
// Panics if cfg or logger are nil.
func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	if cfg == nil {
		panic("daemon: cfg must not be nil")
	}
	if logger == nil {
		panic("daemon: logger must not be nil")
	}
	m := metrics.New()
	d := &Daemon{
		cfg:         cfg,
		logger:      logger,
		metrics:     m,
		rl:          newRateLimiter(),
		bytesRL:     newBytesRateLimiter(),
		vizPipe:     vizpipe.New(1000, &vizDropAdapter{c: m.VizEventsDroppedTotal}, logger),
		eventBus:    eventbus.New(256),
		liteBus:     events.NewLiteBus(512),
		pipeMetrics: newPipelineMetrics(),
		stopped:     make(chan struct{}),
		shutdownReq: make(chan struct{}),
	}
	// -1.0 means "not yet computed". Overwritten on first check.
	d.consistencyScore.Store(math.Float64bits(-1.0))

	// Agent session manager with 30-minute idle timeout (AG.2).
	d.sessionMgr = agent.NewSessionManager(30*time.Minute, logger)

	// Agent activity log with 7-day retention (AG.7).
	d.activityLog = agent.NewActivityLog(7*24*time.Hour, logger)

	// Agent health tracker (AG.8).
	d.healthTracker = agent.NewHealthTracker(logger)

	return d
}

// getConfig returns the current *config.Config under RLock. All concurrent
// accesses to cfg must go through this method to be race-free during hot reload.
//
// The returned pointer is to an immutable Config struct — hot reload only swaps
// the pointer, never mutates in-place. So callers may dereference fields after
// releasing the RLock.
func (d *Daemon) getConfig() *config.Config {
	d.configMu.RLock()
	c := d.cfg
	d.configMu.RUnlock()
	return c
}

// Start opens the WAL, opens the destination, replays pending WAL entries,
// starts the queue workers, starts the hot reload watcher, and starts the HTTP
// server. It blocks until the HTTP server returns (i.e. until Stop is called
// or the listener fails).
//
// Start is not safe to call concurrently. Call it once per Daemon.
func (d *Daemon) Start() error {
	d.startedAt = time.Now()

	cfg := d.getConfig()

	// Verify config signatures if signing is enabled.
	// Reference: Tech Spec Section 6.5 — refuse to start if any compiled
	// config file has a missing or invalid signature.
	if cfg.Daemon.Signing.Enabled {
		if cfg.Daemon.Signing.KeyFile == "" {
			return fmt.Errorf("daemon: config signing enabled but key_file is missing")
		}
		resolvedSignKey, resolveErr := config.ResolveEnv(cfg.Daemon.Signing.KeyFile, d.logger)
		if resolveErr != nil {
			return fmt.Errorf("daemon: resolve signing key_file: %w", resolveErr)
		}
		if resolvedSignKey == "" {
			return fmt.Errorf("daemon: signing key_file resolved to empty value")
		}
		d.signingKey = []byte(resolvedSignKey)

		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("daemon: resolve config dir for signing verification: %w", err)
		}
		compiledDir := filepath.Join(configDir, "compiled")

		onEvent := func(eventType string, attrs ...slog.Attr) {
			d.logger.LogAttrs(context.Background(), slog.LevelWarn, "daemon: security event",
				append([]slog.Attr{
					slog.String("component", "signing"),
					slog.String("event_type", eventType),
				}, attrs...)...,
			)
			if d.securityLog != nil {
				details := make(map[string]interface{}, len(attrs))
				for _, a := range attrs {
					details[a.Key] = a.Value.Any()
				}
				d.securityLog.Emit(securitylog.Event{
					EventType: eventType,
					Details:   details,
				})
			}
		}

		if err := signing.VerifyAll(compiledDir, d.signingKey, onEvent, d.logger); err != nil {
			return fmt.Errorf("daemon: config signature verification failed — refusing to start: %w", err)
		}
		d.logger.Info("daemon: config signature verification passed",
			"component", "daemon",
		)
	}

	// Open WAL.
	walPath, err := d.resolveWALPath()
	if err != nil {
		return fmt.Errorf("daemon: resolve WAL path: %w", err)
	}

	d.logger.Info("daemon: opening WAL",
		"component", "daemon",
		"path", walPath,
	)

	// Build WAL options from config.
	var walOpts []wal.Option
	if cfg.Daemon.WAL.Integrity.Mode == wal.IntegrityModeMAC {
		if cfg.Daemon.WAL.Integrity.MacKeyFile == "" {
			return fmt.Errorf("daemon: integrity mode %q requires mac_key_file", wal.IntegrityModeMAC)
		}
		resolved, resolveErr := config.ResolveEnv(cfg.Daemon.WAL.Integrity.MacKeyFile, d.logger)
		if resolveErr != nil {
			return fmt.Errorf("daemon: resolve WAL mac_key_file: %w", resolveErr)
		}
		if resolved == "" {
			return fmt.Errorf("daemon: WAL mac_key_file resolved to empty value")
		}
		walOpts = append(walOpts, wal.WithIntegrity(wal.IntegrityModeMAC, []byte(resolved)))
		d.logger.Info("daemon: WAL integrity mode enabled",
			"component", "daemon",
			"mode", wal.IntegrityModeMAC,
		)
	}
	// WAL encryption: AES-256-GCM at-rest encryption.
	// Reference: Tech Spec Section 6.4.2.
	if cfg.Daemon.WAL.Encryption.Enabled {
		if cfg.Daemon.WAL.Encryption.KeyFile == "" {
			return fmt.Errorf("daemon: WAL encryption enabled but key_file is missing")
		}
		resolved, resolveErr := config.ResolveEnv(cfg.Daemon.WAL.Encryption.KeyFile, d.logger)
		if resolveErr != nil {
			return fmt.Errorf("daemon: resolve WAL encryption key_file: %w", resolveErr)
		}
		if resolved == "" {
			return fmt.Errorf("daemon: WAL encryption key_file resolved to empty value")
		}
		walOpts = append(walOpts, wal.WithEncryption([]byte(resolved)))
		d.logger.Warn("daemon: WAL encryption configured but NOT YET IMPLEMENTED — WAL data is stored in plaintext",
			"component", "daemon",
		)
	}

	walOpts = append(walOpts, wal.WithSecurityEvent(func(eventType string, attrs ...slog.Attr) {
		d.logger.LogAttrs(context.Background(), slog.LevelWarn, "daemon: security event",
			append([]slog.Attr{
				slog.String("component", "wal"),
				slog.String("event_type", eventType),
			}, attrs...)...,
		)
		if d.securityLog != nil {
			details := make(map[string]interface{}, len(attrs))
			for _, a := range attrs {
				details[a.Key] = a.Value.Any()
			}
			d.securityLog.Emit(securitylog.Event{
				EventType: eventType,
				Details:   details,
			})
		}
	}))

	// Create goroutine heartbeat supervisor. Monitors group commit consumer,
	// queue workers, and WAL watchdog. On stall: logs fatal, dumps stacks,
	// exits with code 3. Converts silent deadlock into visible crash.
	home, _ := os.UserHomeDir()
	logsDir := filepath.Join(home, ".nexus", "logs")
	d.supervisor = supervisor.New(supervisor.Config{
		Timeout: 30 * time.Second,
		LogsDir: logsDir,
	}, d.logger)

	// Enable zstd compression for new WAL entries when configured.
	// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.10.
	if cfg.Daemon.WAL.CompressEnabled {
		walOpts = append(walOpts, wal.WithCompression())
		d.logger.Info("daemon: WAL zstd compression enabled",
			"component", "daemon",
		)
	}

	if cfg.Daemon.WAL.GroupCommit.Enabled {
		d.supervisor.Register("groupcommit")
		gcCfg := wal.GroupCommitConfig{
			Enabled:  true,
			MaxBatch: cfg.Daemon.WAL.GroupCommit.MaxBatch,
			MaxDelay: time.Duration(cfg.Daemon.WAL.GroupCommit.MaxDelayUS) * time.Microsecond,
			BeatFn:   func() { d.supervisor.Beat("groupcommit") },
		}
		walOpts = append(walOpts, wal.WithGroupCommit(gcCfg))
		d.logger.Info("daemon: WAL group commit enabled",
			"component", "daemon",
			"max_batch", gcCfg.MaxBatch,
			"max_delay_us", cfg.Daemon.WAL.GroupCommit.MaxDelayUS,
		)
	}

	// Startup fsync verification: write+sync+read-back test on the WAL
	// filesystem. Logs a warning if fsync appears to be a no-op (broken on
	// network storage, some consumer SSDs). Non-fatal — daemon continues.
	// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.6.
	fsyncResult := doctor.FsyncTest(filepath.Dir(walPath))
	if !fsyncResult.OK {
		d.logger.Warn("daemon: fsync verification FAILED — data durability may be compromised",
			"component", "daemon",
			"error", fsyncResult.Error,
			"wal_dir", filepath.Dir(walPath),
		)
	} else {
		d.logger.Info("daemon: fsync verification passed",
			"component", "daemon",
			"duration", fsyncResult.Duration,
		)
	}

	w, err := wal.Open(walPath, cfg.Daemon.WAL.MaxSegmentSizeMB, d.logger, walOpts...)
	if err != nil {
		return fmt.Errorf("daemon: open WAL: %w", err)
	}
	d.wal = w

	// Open destination adapter (factory selects backend from config type).
	destCfg := d.resolveDestinationConfig()
	configDir, configDirErr := config.ConfigDir()
	if configDirErr != nil {
		return fmt.Errorf("daemon: resolve config dir: %w", configDirErr)
	}

	d.logger.Info("daemon: opening destination",
		"component", "daemon",
		"type", destCfg.Type,
		"name", destCfg.Name,
	)

	openedDest, err := destfactory.OpenByType(destCfg, d.logger, configDir)
	if err != nil {
		return fmt.Errorf("daemon: open destination: %w", err)
	}
	d.dest = openedDest
	if q, ok := openedDest.(destination.Querier); ok {
		d.querier = q
	}

	// WIRE.3: run schema migrations on startup (idempotent, records in nexus_migrations).
	if migrateErr := openedDest.Migrate(context.Background(), 0); migrateErr != nil {
		d.logger.Warn("daemon: schema migration failed (non-fatal)",
			"component", "daemon", "error", migrateErr)
	}

	// CU.0.2: wire memory content encryption via MasterKeyManager.
	// Password is resolved from NEXUS_PASSWORD env (set by `nexus config set-password`).
	if home, homeErr := os.UserHomeDir(); homeErr != nil {
		d.logger.Warn("daemon: cannot resolve home directory; memory encryption disabled",
			"component", "daemon", "error", homeErr)
	} else {
		saltPath := filepath.Join(home, ".nexus", "crypto.salt")
		mkm, mkmErr := nexuscrypto.NewMasterKeyManager("", saltPath)
		if mkmErr != nil {
			d.logger.Warn("daemon: master key derivation failed; memory encryption disabled",
				"component", "daemon", "error", mkmErr)
		} else {
			// CU.0.11: startup encryption self-test — refuse to start if the
			// crypto stack cannot round-trip its own keys.
			if selfErr := nexuscrypto.SelfTest(mkm); selfErr != nil {
				return fmt.Errorf("daemon: encryption self-test failed — refusing to start: %w", selfErr)
			}
			d.mkm = mkm // shared with control-plane encryption (CU.0.4)
			if sqliteDst, ok := d.dest.(*destination.SQLiteDestination); ok {
				sqliteDst.SetEncryption(mkm)
			}
			if mkm.IsEnabled() {
				d.logger.Info("daemon: memory content encryption enabled (self-test passed)",
					"component", "daemon")
			} else {
				d.logger.Warn("daemon: memory content encryption DISABLED — set NEXUS_PASSWORD to enable",
					"component", "daemon")
			}
		}
	}

	// DEF.2: wire Tier-0 immune scanner (always-on, zero config needed).
	d.immuneScanner = immune.New()

	// DEF.2: open quarantine store. Failure is non-fatal; quarantine routes
	// simply do not register when quarantineStore is nil.
	if configDir, cdErr := config.ConfigDir(); cdErr == nil {
		qPath := filepath.Join(configDir, "quarantine.db")
		if qs, qErr := quarantine.New(qPath); qErr != nil {
			d.logger.Warn("daemon: cannot open quarantine store — quarantine features disabled",
				"component", "quarantine",
				"path", qPath,
				"error", qErr,
			)
		} else {
			d.quarantineStore = qs
			d.logger.Info("daemon: quarantine store opened",
				"component", "quarantine",
				"path", qPath,
			)
		}
	}

	// WEB.2: init discovery scanner for GET /api/discover/results.
	if configDir, cdErr := config.ConfigDir(); cdErr == nil {
		d.discoveryScanner = discover.NewScanner(configDir, d.logger)
	}

	// W1: initialise path allowlist for closed action set.
	if err := maintain.InitAllowedPaths(); err != nil {
		d.logger.Warn("daemon: maintain path allowlist init failed", "err", err)
	}
	// W4: recover any incomplete transactions from a prior crash.
	if err := maintain.RecoverIncomplete(context.Background()); err != nil {
		d.logger.Warn("daemon: maintain recover incomplete transactions", "err", err)
	}

	// Create embedding client from config. Returns nil when disabled.
	// INVARIANT: resolved API key is never logged.
	if cfg.Daemon.Embedding.Enabled {
		resolvedEmbedKey, resolveErr := config.ResolveEnv(cfg.Daemon.Embedding.APIKey, d.logger)
		if resolveErr != nil {
			d.logger.Warn("daemon: embedding API key resolve failed; semantic retrieval disabled",
				"component", "daemon",
				"error", resolveErr,
			)
		} else {
			ec, ecErr := embedding.NewClient(cfg.Daemon.Embedding, resolvedEmbedKey, d.logger)
			if ecErr != nil {
				d.logger.Warn("daemon: embedding client creation failed; semantic retrieval disabled",
					"component", "daemon",
					"error", ecErr,
				)
			} else {
				d.embeddingClient = ec
			}
		}
	}

	// Initialise exact cache (Stage 1) and semantic cache (Stage 2).
	// Store stats refs on the daemon for admin endpoint access.
	d.exactStats = cache.NewStats(d.metrics.Registry())
	d.semanticStats = cache.NewSemanticStats(d.metrics.Registry())
	d.exactCache = cache.NewExactCache(cache.DefaultMaxBytes, d.exactStats)
	d.semanticCache = cache.NewSemanticCache(cache.DefaultSemanticMaxEntries, d.semanticStats)

	// Initialise idempotency store.
	d.idem = idempotency.New()

	// Create queue — wire OnProcessed to increment queue_processing_rate,
	// and OnDelivered to advance cache watermarks so stale entries are
	// invalidated.
	d.supervisor.Register("queue")
	d.queue = queue.New(
		queue.Config{
			Size:        cfg.Daemon.QueueSize,
			OnProcessed: d.metrics.QueueProcessingRate.Inc,
			OnDelivered: func(dest string) {
				d.exactCache.InvalidateDest(dest)
				d.semanticCache.InvalidateDest(dest)
			},
			OnSubstrateWrite: func(tp destination.TranslatedPayload) {
				// Substrate write hook: compute sketch + encrypt embedding.
				// d.substrate is captured by reference and checked at call time
				// (it's set later in the startup sequence).
				if d.substrate == nil || !d.substrate.Enabled() {
					return
				}
				// Convert float32 embedding to float64 for canonical pipeline.
				emb := make([]float64, len(tp.Embedding))
				for i, v := range tp.Embedding {
					emb[i] = float64(v)
				}
				if err := d.substrate.ComputeAndStoreSketch(tp.PayloadID, emb, tp.Source); err != nil {
					d.logger.Warn("substrate sketch write failed",
						"component", "queue",
						"memory_id", tp.PayloadID,
						"error", err,
					)
				}
			},
			BeatFn: func() { d.supervisor.Beat("queue") },
		},
		d.logger,
		d.dest,
		d.wal,
	)

	// Replay WAL: re-register idempotency keys and re-enqueue PENDING entries.
	// Measure replay duration for the nexus_replay_duration_seconds gauge.
	if err := d.replayWAL(); err != nil {
		return fmt.Errorf("daemon: WAL replay: %w", err)
	}

	// Set initial WAL metrics.
	d.metrics.WALCRCFailures.Add(float64(d.wal.CRCFailures()))
	d.metrics.WALIntegrityFailures.Add(float64(d.wal.IntegrityFailures()))
	d.metrics.WALHealthy.Set(1)
	d.walHealthy.Store(1)

	// Start WAL watchdog — updates WAL health and disk metrics periodically.
	// Reference: Tech Spec Section 4.4.
	d.supervisor.Register("walwatchdog")
	go d.walWatchdog(walPath)

	// Start the goroutine heartbeat supervisor now that all monitored
	// goroutines are registered.
	d.supervisor.Start()

	// Start consistency checker if enabled.
	// Reference: Tech Spec Section 11.5.
	if cfg.Consistency.Enabled {
		go d.consistencyChecker()
		d.logger.Info("daemon: consistency checker started",
			"component", "daemon",
			"interval_seconds", cfg.Consistency.IntervalSeconds,
			"sample_size", cfg.Consistency.SampleSize,
		)
	}

	// Start visualization pipe, WebUI activity event bus, and LiteBus bridge.
	d.vizPipe.Start()
	d.eventBus.Start()
	go d.runLiteBusBridge()

	// Initialise provenance: daemon Ed25519 key, source signing keys, and
	// audit hash chain state. Non-fatal — the daemon runs without provenance
	// if secrets directory is inaccessible (e.g. first install).
	// Reference: v0.1.3 Build Plan Phase 4 Subtasks 4.1–4.3.
	d.initProvenance(cfg)

	// Initialise structured security event logger if enabled.
	// Reference: Tech Spec Section 11.2, Section 9.2.18.
	if cfg.SecurityEvents.Enabled && cfg.SecurityEvents.LogFile != "" {
		logFile := cfg.SecurityEvents.LogFile
		// Expand ~ prefix to the user home directory.
		if strings.HasPrefix(logFile, "~/") || strings.HasPrefix(logFile, "~\\") {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return fmt.Errorf("daemon: expand security events log_file: %w", homeErr)
			}
			logFile = filepath.Join(home, logFile[2:])
		}
		sl, slErr := securitylog.New(logFile, d.logger)
		if slErr != nil {
			return fmt.Errorf("daemon: open security event log %q: %w", logFile, slErr)
		}
		d.securityLog = sl
		d.logger.Info("daemon: security event logging enabled",
			"component", "daemon",
			"log_file", logFile,
		)
	}

	// Initialise retrieval firewall if enabled.
	// Reference: Tech Spec Addendum Section A3.1.
	if cfg.Daemon.RetrievalFirewall.Enabled {
		d.retrievalFirewall = firewall.New(cfg.Daemon.RetrievalFirewall, d.logger).
			WithMetrics(
				d.metrics.FirewallFilteredTotal,
				d.metrics.FirewallDeniedTotal,
				d.metrics.FirewallLatency,
			)
		d.logger.Info("daemon: retrieval firewall enabled",
			"component", "daemon",
			"tier_order", cfg.Daemon.RetrievalFirewall.TierOrder,
			"default_tier", cfg.Daemon.RetrievalFirewall.DefaultTier,
		)
	}

	// Initialise audit logger and reader if enabled.
	// Reference: Tech Spec Addendum Section A2.3, A2.5.
	if cfg.Daemon.Audit.Enabled {
		logFile := cfg.Daemon.Audit.LogFile
		if logFile == "" {
			return fmt.Errorf("daemon: audit enabled but log_file is empty after config load — this indicates a config loader bug")
		}
		// Expand ~ prefix to the user home directory.
		if strings.HasPrefix(logFile, "~/") || strings.HasPrefix(logFile, "~\\") {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return fmt.Errorf("daemon: expand audit log_file: %w", homeErr)
			}
			logFile = filepath.Join(home, logFile[2:])
		}

		var auditOpts []audit.LoggerOption
		auditOpts = append(auditOpts, audit.WithLogger(d.logger))

		maxSize := int64(cfg.Daemon.Audit.MaxFileSizeMB) * 1024 * 1024
		if maxSize > 0 {
			auditOpts = append(auditOpts, audit.WithMaxFileSize(maxSize))
		}
		auditOpts = append(auditOpts, audit.WithDualWrite(cfg.Daemon.Audit.AuditDualWriteEnabled()))

		// Audit integrity: SEPARATE key from WAL.
		if cfg.Daemon.Audit.Integrity.Mode == "mac" {
			if cfg.Daemon.Audit.Integrity.MacKeyFile == "" {
				return fmt.Errorf("daemon: audit integrity mode %q requires mac_key_file", "mac")
			}
			resolved, resolveErr := config.ResolveEnv(cfg.Daemon.Audit.Integrity.MacKeyFile, d.logger)
			if resolveErr != nil {
				return fmt.Errorf("daemon: resolve audit mac_key_file: %w", resolveErr)
			}
			auditOpts = append(auditOpts, audit.WithIntegrityMode("mac", []byte(resolved)))
		}

		// Audit encryption: SEPARATE key from WAL.
		if cfg.Daemon.Audit.Encryption.Enabled {
			if cfg.Daemon.Audit.Encryption.KeyFile == "" {
				return fmt.Errorf("daemon: audit encryption enabled but key_file is missing")
			}
			resolved, resolveErr := config.ResolveEnv(cfg.Daemon.Audit.Encryption.KeyFile, d.logger)
			if resolveErr != nil {
				return fmt.Errorf("daemon: resolve audit encryption key_file: %w", resolveErr)
			}
			auditOpts = append(auditOpts, audit.WithEncryption([]byte(resolved)))
		}

		al, alErr := audit.NewAuditLogger(logFile, auditOpts...)
		if alErr != nil {
			return fmt.Errorf("daemon: open audit logger: %w", alErr)
		}
		d.auditLogger = al
		d.auditWAL = audit.NewWALWriter(d.wal, d.chainState)
		d.auditWAL.SetEncryption(d.mkm) // CU.0.5: nil mkm or disabled → no-op

		// Build reader options mirroring the logger config.
		var readerOpts []audit.ReaderOption
		readerOpts = append(readerOpts, audit.WithReaderLogger(d.logger))
		readerOpts = append(readerOpts, audit.WithReaderDualWrite(cfg.Daemon.Audit.AuditDualWriteEnabled()))
		if cfg.Daemon.Audit.Integrity.Mode == "mac" {
			resolved, _ := config.ResolveEnv(cfg.Daemon.Audit.Integrity.MacKeyFile, d.logger)
			readerOpts = append(readerOpts, audit.WithReaderIntegrity("mac", []byte(resolved)))
		}
		if cfg.Daemon.Audit.Encryption.Enabled {
			resolved, _ := config.ResolveEnv(cfg.Daemon.Audit.Encryption.KeyFile, d.logger)
			readerOpts = append(readerOpts, audit.WithReaderEncryption([]byte(resolved)))
		}
		d.auditReader = audit.NewAuditReader(logFile, readerOpts...)
		d.auditRateLimiter = newRateLimiter()

		d.logger.Info("daemon: audit interaction log enabled",
			"component", "daemon",
			"log_file", logFile,
			"max_file_size_mb", cfg.Daemon.Audit.MaxFileSizeMB,
			"dual_write", cfg.Daemon.Audit.AuditDualWriteEnabled(),
		)
	}

	// Initialise event sink (webhooks) if enabled.
	// Reference: Tech Spec Section 10.
	if cfg.Daemon.Events.Enabled && len(cfg.Daemon.Events.Sinks) > 0 {
		sinks := make([]eventsink.SinkConfig, len(cfg.Daemon.Events.Sinks))
		for i, s := range cfg.Daemon.Events.Sinks {
			sinks[i] = eventsink.SinkConfig{
				Name:           s.Name,
				Type:           s.Type,
				URL:            s.URL,
				TimeoutSeconds: s.TimeoutSeconds,
				MaxRetries:     s.MaxRetries,
				Content:        s.Content,
				Facility:       s.Facility,
				Tag:            s.Tag,
				Headers:        s.Headers,
			}
		}
		d.eventSink = eventsink.New(eventsink.Config{
			MaxInFlight:         cfg.Daemon.Events.MaxInFlight,
			RetryBackoffSeconds: cfg.Daemon.Events.RetryBackoffSeconds,
			Sinks:               sinks,
			Metrics:             &eventSinkMetrics{m: d.metrics},
			Logger:              d.logger,
		})
		d.eventSink.Start()
		d.logger.Info("daemon: event sink started",
			"component", "daemon",
			"sinks", len(sinks),
			"max_inflight", cfg.Daemon.Events.MaxInFlight,
		)
	}

	// Start hot reload watcher.
	d.startHotReload()

	// Start MCP server if configured. Failure is non-fatal — the daemon MUST
	// continue running even if MCP cannot bind.
	// Reference: Tech Spec Section 14.3 — "Startup failure does NOT crash daemon."
	d.startMCPServer(cfg)

	// Start Agent Gateway subsystems (AG.1–AG.8). Must be after MCP server
	// start so we can wire coordination provider and policy checker.
	d.startAgentGateway()

	// ── BF-Sketch substrate initialization ──────────────────────────────
	// Substrate is disabled by default. When enabled, the full initialization
	// runs (BS.2+). Any startup error disables substrate and the daemon
	// continues with the legacy cascade (fail-closed, rule 4).
	// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 0 rule 4.
	{
		canonicalCfg := canonical.DefaultConfig()
		if cfg.Canonical.Enabled {
			canonicalCfg.Enabled = true
			canonicalCfg.CanonicalDim = cfg.Canonical.CanonicalDim
			if canonicalCfg.CanonicalDim == 0 {
				canonicalCfg.CanonicalDim = 1024
			}
			canonicalCfg.WhiteningWarmup = cfg.Canonical.WhiteningWarmup
			if canonicalCfg.WhiteningWarmup == 0 {
				canonicalCfg.WhiteningWarmup = 1000
			}
			canonicalCfg.QueryCacheTTLSeconds = cfg.Canonical.QueryCacheTTLSeconds
			if canonicalCfg.QueryCacheTTLSeconds == 0 {
				canonicalCfg.QueryCacheTTLSeconds = 60
			}
		}

		substrateCfg := substrate.DefaultConfig()
		if cfg.Substrate.Enabled {
			substrateCfg.Enabled = true
			substrateCfg.SketchBits = cfg.Substrate.SketchBits
			if substrateCfg.SketchBits == 0 {
				substrateCfg.SketchBits = 1
			}
			if cfg.Substrate.RatchetRotationPeriod != "" {
				substrateCfg.RatchetRotationPeriodStr = cfg.Substrate.RatchetRotationPeriod
			}
			if cfg.Substrate.PrefilterThreshold > 0 {
				substrateCfg.PrefilterThreshold = cfg.Substrate.PrefilterThreshold
			}
			if cfg.Substrate.PrefilterTopK > 0 {
				substrateCfg.PrefilterTopK = cfg.Substrate.PrefilterTopK
			}
			if cfg.Substrate.CuckooCapacity > 0 {
				substrateCfg.CuckooCapacity = cfg.Substrate.CuckooCapacity
			}
			if cfg.Substrate.CuckooRebuildThreshold > 0 {
				substrateCfg.CuckooRebuildThreshold = cfg.Substrate.CuckooRebuildThreshold
			}
			substrateCfg.EncryptionEnabled = cfg.Substrate.EncryptionEnabled
		}

		// Substrate requires canonical; force-enable if needed.
		if substrateCfg.Enabled && !canonicalCfg.Enabled {
			d.logger.Warn("substrate enabled without canonical; force-enabling canonical",
				"component", "daemon")
			canonicalCfg.Enabled = true
		}

		if err := canonicalCfg.Validate(); err != nil {
			d.logger.Warn("canonical config invalid, disabling canonical",
				"component", "daemon", "error", err)
			canonicalCfg = canonical.DefaultConfig()
			substrateCfg.Enabled = false
			substrateCfg = substrate.DefaultConfig()
		}
		if err := substrateCfg.Validate(); err != nil {
			d.logger.Warn("substrate config invalid, disabling substrate",
				"component", "daemon", "error", err)
			substrateCfg = substrate.DefaultConfig()
		}

		d.canonical = canonical.NewManager(canonicalCfg)
		if d.canonical != nil && substrateCfg.Enabled {
			// Canonical needs Init with secrets dir
			home, homeErr := os.UserHomeDir()
			if homeErr == nil {
				basePath := filepath.Join(home, ".nexus", "Nexus")
				sd, sdErr := secrets.Open(basePath)
				if sdErr == nil {
					if initErr := d.canonical.Init(sd, d.logger); initErr != nil {
						d.logger.Warn("canonical init failed, disabling substrate",
							"component", "daemon", "error", initErr)
						substrateCfg.Enabled = false
					}

					if substrateCfg.Enabled {
						// Get the SQLite DB handle for substrate operations
						var sqlDB *sql.DB
						if sqliteDst, ok := d.dest.(*destination.SQLiteDestination); ok {
							sqlDB = sqliteDst.DB()
						}
						// CU.0.9: wire substrate state encryption if mkm is available.
						var subOpts []substrate.Option
						if d.mkm != nil {
							enc := substrate.NewSubstrateEncryptor(d.mkm)
							if enc != nil {
								subOpts = append(subOpts, substrate.WithEncryptor(enc))
								d.logger.Info("daemon: substrate state encryption enabled",
									"component", "substrate")
							}
						}
						sub, subErr := substrate.New(
							substrateCfg, sqlDB, sd,
							d.daemonKeyPair, d.canonical,
							d.chainState, d.logger,
							subOpts...,
						)
						if subErr != nil {
							d.logger.Warn("substrate initialization failed, disabling substrate",
								"component", "daemon", "error", subErr)
						} else {
							d.substrate = sub
						}
					}
				} else {
					d.logger.Warn("substrate: cannot open secrets dir, disabling",
						"component", "daemon", "error", sdErr)
				}
			} else {
				d.logger.Warn("substrate: cannot resolve home dir, disabling",
					"component", "daemon", "error", homeErr)
			}
		}

		if d.canonical == nil || !substrateCfg.Enabled {
			// Disabled path — create disabled stub
			disabledCfg := substrate.DefaultConfig()
			d.substrate, _ = substrate.New(disabledCfg, nil, nil, nil, nil, nil, d.logger)
		}

		if d.substrate != nil && d.substrate.Enabled() {
			d.logger.Info("substrate enabled", "component", "daemon")
		}
	}

	// Initialise JWT validator if enabled.
	// Reference: Tech Spec Section 6.6.
	if cfg.Daemon.JWT.Enabled {
		if cfg.Daemon.JWT.JWKSUrl == "" {
			return fmt.Errorf("daemon: JWT enabled but jwks_url is empty")
		}
		v := jwtauth.New(jwtauth.Config{
			JWKSUrl:       cfg.Daemon.JWT.JWKSUrl,
			ClaimToSource: cfg.Daemon.JWT.ClaimToSource,
			Audience:      cfg.Daemon.JWT.Audience,
			Logger:        d.logger,
		})
		if err := v.FetchJWKS(); err != nil {
			// Non-fatal — log warning and continue. Keys will be refreshed on
			// first request that fails validation.
			d.logger.Warn("daemon: initial JWKS fetch failed — JWT auth may fail until refresh succeeds",
				"component", "daemon",
				"error", err,
			)
		}
		d.jwtValidator = v
		d.logger.Info("daemon: JWT authentication enabled",
			"component", "daemon",
			"jwks_url", cfg.Daemon.JWT.JWKSUrl,
			"claim_to_source", cfg.Daemon.JWT.ClaimToSource,
		)
	}

	// Parse trusted proxies for effective_client_ip resolution.
	// Reference: Tech Spec Section 6.3.
	proxies, err := parseTrustedProxies(cfg.Daemon.TrustedProxies)
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}
	d.proxies = proxies
	if len(proxies.networks) > 0 {
		d.logger.Info("daemon: trusted proxies configured",
			"component", "daemon",
			"cidr_count", len(proxies.networks),
		)
	}

	// Build TLS configuration if enabled.
	// INVARIANT: If TLS enabled but certs missing/unreadable, refuse to start.
	// Reference: Tech Spec Section 6.2.
	resolve := func(ref string) (string, error) {
		return config.ResolveEnv(ref, d.logger)
	}
	tlsCfg, err := buildTLSConfig(cfg.Daemon.TLS, resolve)
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}

	// Open the A2A registry store unconditionally before the router is built.
	// The registry holds agent identity (foundational infra) and its *sql.DB
	// is shared with the control-plane stores so grants/approvals/tasks/actions
	// foreign-key against the real a2a_agents table. A2A bridge setup later
	// reuses the same store. Failure logs a warning and proceeds — control
	// routes simply do not register, and A2A setup is skipped.
	if configDir, cdErr := config.ConfigDir(); cdErr == nil {
		regPath := filepath.Join(configDir, "a2a", "registry.db")
		if err := os.MkdirAll(filepath.Dir(regPath), 0o700); err != nil {
			d.logger.Warn("daemon: cannot create registry dir — control plane and A2A disabled",
				"component", "registry",
				"path", filepath.Dir(regPath),
				"error", err,
			)
		} else if rs, err := registry.NewStore(regPath); err != nil {
			d.logger.Warn("daemon: cannot open A2A registry — control plane and A2A disabled",
				"component", "registry",
				"path", regPath,
				"error", err,
			)
		} else {
			d.registryStore = rs
			// CU.0.4/CU.0.6: add encrypted columns to existing DBs; no-op on new ones.
			if migErr := registry.MigrateEncryptionColumns(rs.DB()); migErr != nil {
				d.logger.Warn("daemon: control plane schema migration failed",
					"component", "control", "error", migErr)
			}
			// CU.0.6: wire agent registry encryption (always, regardless of control plane).
			if d.mkm != nil {
				d.registryStore.SetEncryption(d.mkm)
				if d.mkm.IsEnabled() {
					d.logger.Info("daemon: agent registry encryption enabled",
						"component", "registry")
				}
			}
			if cfg.Control.Enabled {
				db := rs.DB()
				d.grantStore = grants.NewStore(db)
				d.approvalStore = approvals.NewStore(db)
				d.taskStore = tasks.NewStore(db)
				d.actionStore = actions.NewStore(db)
				if d.mkm != nil {
					d.grantStore.SetEncryption(d.mkm)
					d.approvalStore.SetEncryption(d.mkm)
					d.taskStore.SetEncryption(d.mkm)
					d.actionStore.SetEncryption(d.mkm)
				}
				d.policyEngine = policy.NewEngine(rs, d.grantStore, d.approvalStore, d.actionStore,
				policy.EngineConfig{RequireApproval: cfg.Control.Capabilities.RequireApproval},
				d.logger)
				if d.mkm != nil && d.mkm.IsEnabled() {
					d.logger.Info("daemon: control plane table encryption enabled",
						"component", "control")
				} else {
					d.logger.Warn("daemon: control plane table encryption DISABLED — set NEXUS_PASSWORD to enable",
						"component", "control")
				}
				d.logger.Info("daemon: control plane initialized",
					"component", "control",
					"path", regPath,
				)
			} else {
				d.logger.Info("daemon: control plane disabled (control.enabled = false)",
					"component", "control",
				)
			}
		}

		if d.registryStore != nil {
			ss, err := subscribe.NewStore(d.registryStore.DB())
			if err != nil {
				d.logger.Warn("daemon: subscribe store init failed", "error", err)
			} else {
				d.subscribeStore = ss
				var embedFn subscribe.EmbedFunc
				if d.embeddingClient != nil {
					embedFn = func(ctx context.Context, text string) ([]float32, error) {
						return d.embeddingClient.Embed(ctx, text)
					}
				}
				d.subscribeMatcher = subscribe.NewMatcher(ss, embedFn)
				d.logger.Info("daemon: subscribe system initialized")
			}
		}
	} else {
		d.logger.Warn("daemon: skipping registry/control setup — cannot resolve config dir",
			"component", "registry",
			"error", cdErr,
		)
	}

	// Wire the MCP control-plane adapter when both the policy engine and MCP
	// server are available. Must run after the registry init block above and
	// after startMCPServer (called before startAgentGateway at line ~782).
	if d.policyEngine != nil && d.mcpServer != nil {
		d.mcpServer.SetControlPlane(&controlPlaneAdapter{
			engine:    d.policyEngine,
			grants:    d.grantStore,
			approvals: d.approvalStore,
			tasks:     d.taskStore,
			actions:   d.actionStore,
			logger:    d.logger,
		})
		d.logger.Info("daemon: MCP control-plane tools enabled",
			"component", "control",
		)
	}

	// Wire the orchestration engine when the registry and grant stores are available.
	if d.registryStore != nil && d.grantStore != nil && d.mcpServer != nil {
		orchEngine := orchestrate.New(orchestrate.Config{
			Agents: &registryAgentLister{store: d.registryStore},
			Grants: &grantStoreChecker{store: d.grantStore},
			Logger: d.logger,
		})
		d.mcpServer.SetOrchestrateProvider(&orchestrateAdapter{engine: orchEngine})
		d.logger.Info("daemon: MCP orchestration tools enabled",
			"component", "orchestrate",
		)
	}

	if d.subscribeStore != nil && d.mcpServer != nil {
		d.mcpServer.SetSubscribeStore(&subscribeStoreAdapter{store: d.subscribeStore})
	}

	// Wire the A2A bridge if enabled. Uses the already-opened registryStore.
	if cfg.A2A.Enabled {
		d.setupA2ABridge(cfg)
	}

	// Build HTTP server.
	router := d.buildRouter()
	d.server = newHTTPServer(d.serverAddr(), router)

	if tlsCfg != nil {
		d.server.TLSConfig = tlsCfg
		d.logger.Info("daemon: starting HTTPS server (TLS enabled)",
			"component", "daemon",
			"addr", d.serverAddr(),
			"version", version.Version,
			"tls_min", cfg.Daemon.TLS.MinVersion,
			"tls_max", cfg.Daemon.TLS.MaxVersion,
			"client_auth", cfg.Daemon.TLS.ClientAuth,
		)
		// ListenAndServeTLS with empty cert/key paths because the certificate
		// is already loaded in TLSConfig.Certificates.
		if err := d.server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("daemon: HTTPS server: %w", err)
		}
	} else {
		d.logger.Info("daemon: starting HTTP server",
			"component", "daemon",
			"addr", d.serverAddr(),
			"version", version.Version,
		)
		// ListenAndServe blocks until the server is closed.
		if err := d.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("daemon: HTTP server: %w", err)
		}
	}

	return nil
}

// Stop gracefully shuts down the daemon in three budgeted stages. It is safe
// to call multiple times; only the first call has any effect (sync.Once).
//
// Shutdown stages (reference: Tech Spec Section 14.2):
//
//	Stage 1 (stageTimeout): Stop accepting new HTTP requests.
//	Stage 2 (stageTimeout): Drain queue workers.
//	Stage 3 (stageTimeout): Stop reload watcher + close WAL + close destination.
//
// Total budget = drain_timeout_seconds (default 30s). Each stage gets 1/3.
func (d *Daemon) Stop() error {
	var firstErr error

	d.stopOnce.Do(func() {
		defer close(d.stopped)

		cfg := d.getConfig()
		drainTimeout := time.Duration(cfg.Daemon.Shutdown.DrainTimeoutSeconds) * time.Second
		if drainTimeout <= 0 {
			drainTimeout = 30 * time.Second
		}
		// Each stage gets an equal share of the total budget.
		stageTimeout := drainTimeout / 3
		if stageTimeout < 5*time.Second {
			stageTimeout = 5 * time.Second
		}

		d.logger.Info("daemon: shutting down",
			"component", "daemon",
			"drain_timeout", drainTimeout,
			"stage_timeout", stageTimeout,
		)

		// Notify supervisor that shutdown has begun — suppresses stall
		// detection for goroutines that are legitimately draining.
		if d.supervisor != nil {
			d.supervisor.Shutdown()
		}

		// ── Stage 1: Stop accepting new HTTP requests ──────────────────────
		if d.server != nil {
			ctx1, cancel1 := context.WithTimeout(context.Background(), stageTimeout)
			defer cancel1()
			if err := d.server.Shutdown(ctx1); err != nil {
				d.logger.Error("daemon: stage 1 HTTP shutdown error",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		// Stop MCP server alongside HTTP (both are in stage 1 — client-facing).
		if d.mcpServer != nil {
			if err := d.mcpServer.Stop(); err != nil {
				d.logger.Error("daemon: MCP server stop error",
					"component", "daemon",
					"error", err,
				)
			}
		}

		d.logger.Info("daemon: stage 1 complete — HTTP server stopped",
			"component", "daemon",
		)

		// ── Stage 2: Drain queue workers ──────────────────────────────────
		if d.queue != nil {
			ctx2, cancel2 := context.WithTimeout(context.Background(), stageTimeout)
			defer cancel2()
			if !d.queue.DrainWithContext(ctx2) {
				d.logger.Warn("daemon: stage 2 queue drain timed out — some entries may be replayed on restart",
					"component", "daemon",
				)
			}
		}
		d.logger.Info("daemon: stage 2 complete — queue drained",
			"component", "daemon",
		)

		// ── Stage 3: Stop reload watcher, close WAL and destination ───────
		if d.reloadWatcher != nil {
			d.reloadWatcher.Stop()
		}

		if d.dest != nil {
			if err := d.dest.Close(); err != nil {
				d.logger.Error("daemon: close destination",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		if d.wal != nil {
			if err := d.wal.Close(); err != nil {
				d.logger.Error("daemon: close WAL",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		// Stop visualization pipe, WebUI activity event bus, and LiteBus.
		if d.vizPipe != nil {
			d.vizPipe.Stop()
		}
		d.liteBus.Close()
		d.eventBus.Stop()

		// Drain event sink workers.
		// Reference: Tech Spec Section 14.2 — drain in Stage 3.
		if d.eventSink != nil {
			d.eventSink.Stop()
		}

		// Close audit logger.
		if d.auditLogger != nil {
			if err := d.auditLogger.Close(); err != nil {
				d.logger.Error("daemon: close audit logger",
					"component", "daemon",
					"error", err,
				)
			}
		}

		// Close security event log.
		if d.securityLog != nil {
			if err := d.securityLog.Close(); err != nil {
				d.logger.Error("daemon: close security event log",
					"component", "daemon",
					"error", err,
				)
			}
		}

		// Stop agent session manager reap goroutine.
		if d.sessionMgr != nil {
			d.sessionMgr.Stop()
		}

		// Stop activity log pruner.
		if d.activityLog != nil {
			d.activityLog.Stop()
		}

		// Stop health tracker.
		if d.healthTracker != nil {
			d.healthTracker.Stop()
		}

		// Stop BF-Sketch substrate and canonical pipeline.
		if d.substrate != nil {
			if err := d.substrate.Shutdown(); err != nil {
				d.logger.Error("daemon: substrate shutdown error",
					"component", "daemon",
					"error", err,
				)
			}
		}
		if d.canonical != nil {
			if err := d.canonical.Shutdown(); err != nil {
				d.logger.Error("daemon: canonical shutdown error",
					"component", "daemon",
					"error", err,
				)
			}
		}

		// Stop agent gateway subsystems.
		d.stopAgentGateway()

		// Close the quarantine store (DEF.2).
		if d.quarantineStore != nil {
			if err := d.quarantineStore.Close(); err != nil {
				d.logger.Error("daemon: close quarantine store",
					"component", "quarantine",
					"error", err,
				)
			}
		}

		// Close the A2A registry store (shared DB for control plane).
		if d.registryStore != nil {
			if err := d.registryStore.Close(); err != nil {
				d.logger.Error("daemon: close registry store",
					"component", "registry",
					"error", err,
				)
			}
		}

		// Stop the goroutine heartbeat supervisor.
		if d.supervisor != nil {
			d.supervisor.Stop()
		}

		d.logger.Info("daemon: stage 3 complete — daemon stopped",
			"component", "daemon",
		)
	})

	return firstErr
}

// Stopped returns a channel that is closed when the daemon has fully stopped.
func (d *Daemon) Stopped() <-chan struct{} {
	return d.stopped
}

// ShutdownRequested returns a channel that is closed when an API-initiated
// shutdown has been requested (via POST /api/shutdown). The start command
// selects on this alongside OS signals.
func (d *Daemon) ShutdownRequested() <-chan struct{} {
	return d.shutdownReq
}

// RequestShutdown signals that the daemon should begin graceful shutdown.
// Safe to call multiple times; only the first close has any effect.
func (d *Daemon) RequestShutdown() {
	select {
	case <-d.shutdownReq:
		// already closed
	default:
		close(d.shutdownReq)
	}
}

// ---------------------------------------------------------------------------
// MCP server startup
// ---------------------------------------------------------------------------

// startMCPServer resolves the MCP API key and starts the MCP server if
// MCPConfig.Enabled is true. Failure is non-fatal: on any error the daemon
// logs a WARN and continues.
//
// INVARIANT: MCP MUST bind to 127.0.0.1. The Server constructor enforces this.
// Reference: Tech Spec Section 14.3.
func (d *Daemon) startMCPServer(cfg *config.Config) {
	if !cfg.Daemon.MCP.Enabled {
		return
	}

	if len(cfg.ResolvedMCPKey) == 0 {
		d.logger.Warn("daemon: MCP enabled but api_key is empty or unresolved — MCP disabled",
			"component", "daemon",
		)
		return
	}

	// Determine bind and port, with safe defaults.
	bind := cfg.Daemon.MCP.Bind
	if bind == "" {
		bind = "127.0.0.1"
	}
	port := cfg.Daemon.MCP.Port
	if port == 0 {
		port = 7474
	}

	sourceName := cfg.Daemon.MCP.SourceName

	// Daemon itself is the Pipeline implementation.
	srv := mcp.New(bind, port, cfg.ResolvedMCPKey, sourceName, d, d.logger)

	// Wire OAuth server if enabled — must happen before Start() so endpoints
	// are registered on the mux.
	d.setupOAuthServer(cfg, srv)

	// Wire MCP TLS if configured (CU.0.7). Must be before Start().
	d.wireMCPTLS(cfg, srv)

	if err := srv.Start(); err != nil {
		d.logger.Warn("daemon: MCP server start failed — MCP disabled, HTTP continues",
			"component", "daemon",
			"error", err,
		)
		return
	}
	d.mcpServer = srv

	d.logger.Info("daemon: MCP server started",
		"component", "daemon",
		"addr", srv.Addr(),
	)
}

// startOAuthServer loads or generates the RSA key and creates the OAuthServer
// if [daemon.oauth] enabled = true. It registers OAuth endpoints on the MCP
// server's HTTP mux and sets the JWT validator.
//
// Must be called BEFORE startMCPServer calls srv.Start() — but in practice the
// daemon calls startMCPServer which creates and starts the MCP server, so we
// wire OAuth before MCP start by modifying startMCPServer to call this first.
//
// Reference: Post-Build Add-On Update Technical Specification Section 6.
func (d *Daemon) setupOAuthServer(cfg *config.Config, srv *mcp.Server) {
	if !cfg.Daemon.OAuth.Enabled {
		return
	}

	oauthCfg := cfg.Daemon.OAuth

	// Resolve private key file path.
	keyFileRef := oauthCfg.PrivateKeyFile
	if keyFileRef == "" {
		configDir, err := config.ConfigDir()
		if err != nil {
			d.logger.Warn("daemon: oauth: cannot resolve config dir for key file",
				"component", "oauth",
				"error", err,
			)
			return
		}
		keyFileRef = "file:" + filepath.Join(configDir, "oauth_private.key")
	}

	// Resolve the file: reference to get the actual path.
	keyPath := keyFileRef
	if strings.HasPrefix(keyPath, "file:") {
		keyPath = strings.TrimPrefix(keyPath, "file:")
	}

	// Expand ~ to home directory.
	if strings.HasPrefix(keyPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			d.logger.Warn("daemon: oauth: cannot resolve home dir",
				"component", "oauth",
				"error", err,
			)
			return
		}
		keyPath = filepath.Join(home, keyPath[1:])
	}

	// Load or generate RSA key.
	var key *rsa.PrivateKey
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		var genErr error
		key, genErr = oauth.GenerateRSAKey()
		if genErr != nil {
			d.logger.Warn("daemon: oauth: failed to generate RSA key",
				"component", "oauth",
				"error", genErr,
			)
			return
		}
		if saveErr := oauth.SaveRSAKey(key, keyPath); saveErr != nil {
			d.logger.Warn("daemon: oauth: failed to save RSA key",
				"component", "oauth",
				"error", saveErr,
			)
			return
		}
		d.logger.Info("oauth: generated RSA-2048 key pair",
			"component", "oauth",
			"path", keyPath,
		)
	} else {
		var loadErr error
		key, loadErr = oauth.LoadRSAKey(keyPath)
		if loadErr != nil {
			d.logger.Warn("daemon: oauth: failed to load RSA key",
				"component", "oauth",
				"error", loadErr,
			)
			return
		}
		d.logger.Info("oauth: loaded RSA key pair",
			"component", "oauth",
			"path", keyPath,
		)
	}

	// Build OAuth config.
	accessTTL := time.Hour
	if oauthCfg.AccessTokenTTLSecs > 0 {
		accessTTL = time.Duration(oauthCfg.AccessTokenTTLSecs) * time.Second
	}
	codeTTL := 5 * time.Minute
	if oauthCfg.AuthCodeTTLSecs > 0 {
		codeTTL = time.Duration(oauthCfg.AuthCodeTTLSecs) * time.Second
	}

	clients := make([]oauth.OAuthClient, len(oauthCfg.Clients))
	for i, c := range oauthCfg.Clients {
		clients[i] = oauth.OAuthClient{
			ClientID:        c.ClientID,
			ClientName:      c.ClientName,
			RedirectURIs:    c.RedirectURIs,
			OAuthSourceName: c.OAuthSourceName,
			AllowedScopes:   c.AllowedScopes,
		}
	}

	oauthSrv := oauth.NewOAuthServer(oauth.OAuthConfig{
		Enabled:        true,
		IssuerURL:      oauthCfg.IssuerURL,
		PrivateKeyFile: keyPath,
		AccessTokenTTL: accessTTL,
		AuthCodeTTL:    codeTTL,
		Clients:        clients,
	}, key, d.logger)

	// Wire OAuth into the MCP server.
	srv.SetOAuthServer(oauthSrv)
	srv.SetOAuthHandlers(oauthSrv)
	srv.SetOAuthIssuerURL(oauthCfg.IssuerURL)

	d.logger.Info("oauth: server started",
		"component", "oauth",
		"issuer", oauthCfg.IssuerURL,
	)
}

// wireMCPTLS configures TLS on the MCP server when [daemon.mcp] tls_enabled = true.
// Uses an operator-provided cert/key when configured, or auto-generates a self-signed
// P-256 cert at ~/.nexus/keys/tls.crt (idempotent).
func (d *Daemon) wireMCPTLS(cfg *config.Config, srv interface{ SetTLSConfig(*tls.Config) }) {
	if !cfg.Daemon.MCP.TLSEnabled {
		return
	}

	certFile := cfg.Daemon.MCP.TLSCertFile
	keyFile := cfg.Daemon.MCP.TLSKeyFile

	if certFile == "" || keyFile == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			d.logger.Warn("daemon: MCP TLS: cannot resolve home dir — MCP TLS disabled",
				"component", "daemon", "error", homeErr)
			return
		}
		keysDir := filepath.Join(home, ".nexus", "keys")
		var certErr error
		certFile, keyFile, certErr = EnsureAutoTLSCert(keysDir)
		if certErr != nil {
			d.logger.Warn("daemon: MCP TLS: cert generation failed — MCP TLS disabled",
				"component", "daemon", "error", certErr)
			return
		}
	} else {
		resolve := func(ref string) (string, error) {
			return config.ResolveEnv(ref, d.logger)
		}
		var err error
		certFile, err = resolve(certFile)
		if err != nil || certFile == "" {
			d.logger.Warn("daemon: MCP TLS: resolve cert_file failed — MCP TLS disabled",
				"component", "daemon", "error", err)
			return
		}
		keyFile, err = resolve(keyFile)
		if err != nil || keyFile == "" {
			d.logger.Warn("daemon: MCP TLS: resolve key_file failed — MCP TLS disabled",
				"component", "daemon", "error", err)
			return
		}
	}

	tlsCfg, err := buildTLSConfig(config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}, func(s string) (string, error) { return s, nil })
	if err != nil {
		d.logger.Warn("daemon: MCP TLS: config error — MCP TLS disabled",
			"component", "daemon", "error", err)
		return
	}
	srv.SetTLSConfig(tlsCfg)
	d.logger.Info("daemon: MCP TLS enabled",
		"component", "daemon",
		"cert", certFile,
	)
}

// ---------------------------------------------------------------------------
// Hot reload
// ---------------------------------------------------------------------------

// startHotReload initialises and starts the hot reload watcher. A failure to
// start (e.g. sources dir does not exist) is non-fatal — the daemon continues
// without hot reload and logs a warning.
func (d *Daemon) startHotReload() {
	configDir, err := config.ConfigDir()
	if err != nil {
		d.logger.Warn("daemon: cannot resolve config dir — hot reload disabled",
			"component", "daemon",
			"error", err,
		)
		return
	}
	sourcesDir := filepath.Join(configDir, "sources")

	reloadFunc := func() (*config.Config, error) {
		return config.Load(configDir, d.logger)
	}

	// Build signing event callback for hot reload (nil when signing disabled).
	var signingEvent signing.SecurityEventFunc
	if d.signingKey != nil {
		signingEvent = func(eventType string, attrs ...slog.Attr) {
			d.logger.LogAttrs(context.Background(), slog.LevelWarn, "daemon: security event",
				append([]slog.Attr{
					slog.String("component", "signing"),
					slog.String("event_type", eventType),
				}, attrs...)...,
			)
		}
	}

	w := hotreload.New(hotreload.Config{
		SourcesDir:  sourcesDir,
		ConfigDir:   configDir,
		Mu:          &d.configMu,
		Snapshot: func() *config.Config {
			// Called by the watcher under RLock — do not acquire additional locks.
			return d.cfg
		},
		Apply: func(c *config.Config) {
			// Called by the watcher under Lock — do not acquire additional locks.
			d.cfg = c
			d.metrics.ConfigLintWarnings.Set(0) // reset on successful reload
		},
		Reload:       reloadFunc,
		SigningKey:   d.signingKey,
		SigningEvent: signingEvent,
		Logger:       d.logger,
	})

	if err := w.Start(); err != nil {
		d.logger.Warn("daemon: hot reload watcher start failed — hot reload disabled",
			"component", "daemon",
			"error", err,
		)
		return
	}
	d.reloadWatcher = w
}

// ---------------------------------------------------------------------------
// WAL watchdog
// ---------------------------------------------------------------------------

// walWatchdog is a background goroutine that periodically checks WAL health:
// directory writeability, disk space, and pending entry count. Interval is
// configurable via [daemon.wal.watchdog] interval_seconds (default 30).
// Reference: Tech Spec Section 4.4.
func (d *Daemon) walWatchdog(walDir string) {
	cfg := d.getConfig()
	interval := cfg.Daemon.WAL.Watchdog.IntervalSeconds
	if interval <= 0 {
		interval = 10
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		if d.supervisor != nil {
			d.supervisor.Beat("walwatchdog")
		}

		select {
		case <-d.stopped:
			return
		case <-ticker.C:
			d.runWatchdogCheck(walDir)
		}
	}
}

// runWatchdogCheck performs a single watchdog iteration: probe WAL writeability,
// check disk space, update pending count, and set Prometheus metrics + atomic
// health flag for /ready. NEVER writes WAL entries — read-only.
// Reference: Tech Spec Section 4.4.
func (d *Daemon) runWatchdogCheck(walDir string) {
	cfg := d.getConfig()
	minDisk := uint64(cfg.Daemon.WAL.Watchdog.MinDiskBytes)
	if minDisk == 0 {
		minDisk = 100 << 20 // 100MB default per spec
	}

	res := doctor.Check(walDir, nil, minDisk) // no destination checks in watchdog

	healthy := true

	// Check WAL directory writeability.
	if res.WALWritable {
		d.metrics.WALHealthy.Set(1)
	} else {
		d.metrics.WALHealthy.Set(0)
		healthy = false
		d.logger.Error("daemon: WAL watchdog: WAL directory not writable",
			"component", "daemon",
			"wal_dir", walDir,
		)
	}

	// Check disk space threshold.
	d.metrics.WALDiskBytesFree.Set(float64(res.DiskFreeBytes))
	if !res.DiskSpaceOK {
		healthy = false
		d.logger.Error("daemon: WAL watchdog: disk space below threshold",
			"component", "daemon",
			"wal_dir", walDir,
			"free_bytes", res.DiskFreeBytes,
			"min_bytes", minDisk,
		)
	}

	// Update WAL pending entries count.
	if d.wal != nil {
		pending := d.wal.PendingCount()
		d.metrics.WALPendingEntries.Set(float64(pending))
	}

	// Update queue depth.
	if d.queue != nil {
		d.metrics.QueueDepth.Set(float64(d.queue.Len()))
	}

	// Set atomic health flag for /ready endpoint.
	prev := d.walHealthy.Load()
	if healthy {
		d.walHealthy.Store(1)
	} else {
		d.walHealthy.Store(0)
	}

	// EVT.2: emit health_changed when status transitions.
	newVal := int32(0)
	if healthy {
		newVal = 1
	}
	if prev != newVal {
		status := "healthy"
		if !healthy {
			status = "unhealthy"
		}
		d.liteBus.Emit("health_changed", map[string]any{"status": status, "component": "wal"})
	}
}

// ---------------------------------------------------------------------------
// WAL replay
// ---------------------------------------------------------------------------

// replayWAL scans the WAL for PENDING entries, re-registers their idempotency
// keys, and re-enqueues them for delivery to the destination. Replay duration
// and entry count are recorded in metrics.
func (d *Daemon) replayWAL() error {
	replayStart := time.Now()
	pending := 0

	err := d.wal.Replay(func(entry wal.Entry) {
		if entry.IdempotencyKey != "" {
			d.idem.Register(entry.IdempotencyKey, entry.PayloadID)
		}
		if err := d.queue.Enqueue(entry); err != nil {
			d.logger.Warn("daemon: WAL replay: queue full during replay",
				"component", "daemon",
				"payload_id", entry.PayloadID,
			)
		}
		pending++
		d.metrics.ReplayEntriesTotal.Inc()
	})
	if err != nil {
		return err
	}

	replayDuration := time.Since(replayStart)
	d.metrics.ReplayDurationSeconds.Set(replayDuration.Seconds())
	d.metrics.WALPendingEntries.Set(float64(pending))

	d.logger.Info("daemon: WAL replay complete",
		"component", "daemon",
		"pending_entries", pending,
		"duration", replayDuration,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Path resolution helpers
// ---------------------------------------------------------------------------

// resolveWALPath expands the configured WAL path (which may contain ~).
// os.UserHomeDir failure is fatal per Phase 0C Behavioral Contract item 17.
func (d *Daemon) resolveWALPath() (string, error) {
	return expandPath(d.getConfig().Daemon.WAL.Path)
}

// resolveDestinationConfig returns the first configured destination, falling
// back to a synthetic SQLite config pointing at the default memories.db path.
func (d *Daemon) resolveDestinationConfig() *config.Destination {
	for _, dst := range d.getConfig().Destinations {
		if dst.Name != "" && dst.Type != "" {
			return dst
		}
	}
	return &config.Destination{Name: "default", Type: "sqlite"}
}

// resolveSQLitePath returns the SQLite database path.
// Checks configured destinations first, then falls back to the default.
// Used by the admin list handler and path tests which require direct SQLite access.
func (d *Daemon) resolveSQLitePath() (string, error) {
	for _, dst := range d.getConfig().Destinations {
		if dst.Type == "sqlite" && dst.DBPath != "" {
			return expandPath(dst.DBPath)
		}
	}
	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "memories.db"), nil
}

// expandPath expands a leading ~ to the user's home directory.
// Returns an error if os.UserHomeDir fails — callers must treat this as fatal.
func expandPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return filepath.Clean(p), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("os.UserHomeDir: %w", err)
	}
	return filepath.Join(home, p[1:]), nil
}

// ---------------------------------------------------------------------------
// Event sink metrics adapter
// ---------------------------------------------------------------------------

// vizDropAdapter adapts a prometheus.Counter to the vizpipe.DropMetric interface.
type vizDropAdapter struct {
	c prometheus.Counter
}

func (a *vizDropAdapter) Inc() { a.c.Inc() }

// eventSinkMetrics adapts the Metrics struct to the eventsink.Metrics interface.
type eventSinkMetrics struct {
	m *metrics.Metrics
}

func (a *eventSinkMetrics) IncDropped()   { a.m.EventsDroppedTotal.Inc() }
func (a *eventSinkMetrics) IncDelivered() { a.m.EventsDeliveredTotal.Inc() }
func (a *eventSinkMetrics) IncFailed()    { a.m.EventsFailedTotal.Inc() }
