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
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/provenance"
	"github.com/BubbleFish-Nexus/internal/secrets"
)

// initProvenance loads or generates the daemon Ed25519 key, source signing keys,
// and audit chain state. All failures are logged but non-fatal — the daemon can
// operate without provenance (unsigned writes, no chain).
//
// Reference: v0.1.3 Build Plan Phase 4 Subtasks 4.1–4.3, 4.8.
func (d *Daemon) initProvenance(cfg *config.Config) {
	home, err := os.UserHomeDir()
	if err != nil {
		d.logger.Warn("daemon: provenance disabled — cannot resolve home dir",
			"component", "daemon",
			"error", err,
		)
		return
	}

	basePath := filepath.Join(home, ".bubblefish", "Nexus")
	sd, err := secrets.Open(basePath)
	if err != nil {
		d.logger.Warn("daemon: provenance disabled — cannot open secrets directory",
			"component", "daemon",
			"error", err,
		)
		return
	}

	// Load or generate daemon-wide Ed25519 key.
	dkp, err := provenance.LoadOrGenerateDaemonKey(sd)
	if err != nil {
		d.logger.Warn("daemon: provenance — daemon key unavailable",
			"component", "daemon",
			"error", err,
		)
	} else {
		d.daemonKeyPair = dkp
		d.logger.Info("daemon: provenance — daemon key loaded",
			"component", "daemon",
			"key_id", dkp.KeyID,
		)
	}

	// Load source signing keys for sources with [source.signing] mode = "local".
	d.sourceKeys = make(map[string]*provenance.KeyPair)
	for _, src := range cfg.Sources {
		if src.Signing.Mode != "local" {
			continue
		}
		skp, sErr := provenance.LoadOrGenerateSourceKey(sd, src.Name)
		if sErr != nil {
			d.logger.Warn("daemon: provenance — source key unavailable",
				"component", "daemon",
				"source", src.Name,
				"error", sErr,
			)
			continue
		}
		d.sourceKeys[src.Name] = skp
		d.logger.Info("daemon: provenance — source key loaded",
			"component", "daemon",
			"source", src.Name,
			"key_id", skp.KeyID,
		)
	}

	// Initialise audit hash chain state.
	dataDir := filepath.Join(basePath, "data")
	cs, found, csErr := provenance.RestoreChainState(dataDir)
	if csErr != nil {
		d.logger.Warn("daemon: provenance — chain state corrupt; creating fresh chain",
			"component", "daemon",
			"error", csErr,
		)
		cs = provenance.NewChainState()
	}
	if !found && d.daemonKeyPair != nil {
		// First startup: create genesis entry.
		genesisJSON, gErr := cs.Genesis(d.daemonKeyPair)
		if gErr != nil {
			d.logger.Warn("daemon: provenance — genesis creation failed",
				"component", "daemon",
				"error", gErr,
			)
		} else {
			d.logger.Info("daemon: provenance — genesis entry created",
				"component", "daemon",
				"genesis_bytes", len(genesisJSON),
			)
			// Persist chain state after genesis.
			if pErr := cs.SaveChainState(dataDir); pErr != nil {
				d.logger.Warn("daemon: provenance — chain state persist failed",
					"component", "daemon",
					"error", pErr,
				)
			}
		}
	} else if found {
		d.logger.Info("daemon: provenance — chain state restored",
			"component", "daemon",
			"entry_count", cs.EntryCount(),
		)
	}
	d.chainState = cs

	// Start daily Merkle root ticker if daemon key is available.
	if d.daemonKeyPair != nil {
		go d.merkleRootTicker(dataDir)
	}
}

// signWriteEnvelope signs the write payload using the source's Ed25519 key
// if signing is enabled for that source. Returns the signature, signing key ID,
// and algorithm. All empty strings if signing is not configured.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.2.
func (d *Daemon) signWriteEnvelope(tp *destination.TranslatedPayload) {
	kp, ok := d.sourceKeys[tp.Source]
	if !ok || kp == nil {
		return
	}

	env := provenance.SignableEnvelope{
		SourceName:     tp.Source,
		Timestamp:      tp.Timestamp.UTC().Format(time.RFC3339Nano),
		IdempotencyKey: tp.IdempotencyKey,
		ContentHash:    provenance.ContentHash(tp.Content),
	}

	sig, err := provenance.SignEnvelope(env, kp.PrivateKey)
	if err != nil {
		d.logger.Warn("daemon: provenance — sign envelope failed",
			"component", "daemon",
			"source", tp.Source,
			"error", err,
		)
		return
	}

	tp.Signature = sig
	tp.SigningKeyID = kp.KeyID
	tp.SignatureAlg = provenance.SignatureAlgEd25519
}

// merkleRootTicker computes a daily Merkle root at midnight UTC and persists it.
// If external anchoring is configured, it will auto-publish to the configured
// GitHub Gist.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.8.
func (d *Daemon) merkleRootTicker(dataDir string) {
	// Compute time until next midnight UTC.
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	for {
		select {
		case <-d.stopped:
			return
		case <-timer.C:
			d.computeDailyMerkleRoot(dataDir)
			// Reset timer for next midnight.
			now := time.Now().UTC()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
			timer.Reset(time.Until(next))
		}
	}
}

// computeDailyMerkleRoot builds and persists the daily Merkle root.
// It uses the chain state's last hash as a single-leaf Merkle tree for now;
// when the audit reader gains date-range filtering, this will expand to
// all audit entries for that date.
func (d *Daemon) computeDailyMerkleRoot(dataDir string) {
	if d.chainState == nil || d.daemonKeyPair == nil {
		return
	}

	date := time.Now().UTC().Add(-1 * time.Second).Format("2006-01-02")

	// Use the chain state's current hash as a summary leaf.
	lastHash := d.chainState.LastHash()
	var leaves [][]byte
	if lastHash != "" {
		leaves = append(leaves, []byte(lastHash))
	}

	root := provenance.BuildDailyMerkleRoot(date, leaves, d.daemonKeyPair)

	if err := provenance.SaveMerkleRoot(dataDir, root); err != nil {
		d.logger.Warn("daemon: provenance — daily Merkle root persist failed",
			"component", "daemon",
			"date", date,
			"error", err,
		)
		return
	}

	d.logger.Info("daemon: provenance — daily Merkle root computed",
		"component", "daemon",
		"date", date,
		"root_hash", root.Root,
	)
}

// sourcePublicKeyHex returns the hex-encoded public key for the given source,
// or empty string if signing is not configured for that source.
func (d *Daemon) sourcePublicKeyHex(sourceName string) string {
	kp, ok := d.sourceKeys[sourceName]
	if !ok || kp == nil {
		return ""
	}
	return hex.EncodeToString(kp.PublicKey)
}
