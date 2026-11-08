// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/GoogleCloudPlatform/scion/pkg/config"
)

// The store keeps substrateAgentRecords and substrateControlTokens on disk,
// so a broker restart does not turn running actors into record-less ones.
// Exec secrets are not stored: they only redact exec errors.
//
// substrateStateFile is empty, and the store off, until the production
// runtime builder enables it, so tests never write to the real home dir.
var (
	substrateStateFile     string
	substrateStateLoadOnce sync.Once
)

const substrateStateVersion = 1

type substrateStateEntry struct {
	Record       *substrateAgentRecord `json:"record"`
	ControlToken string                `json:"controlToken"`
}

type substrateStateDoc struct {
	Version int                            `json:"version"`
	Records map[string]substrateStateEntry `json:"records"`
}

// enableSubstrateStateStore points the store at ~/.scion/substrate and loads
// it, once per process.
func enableSubstrateStateStore() {
	substrateStateLoadOnce.Do(func() {
		dir, err := config.GetGlobalDir()
		if err != nil {
			slog.Warn("substrate: agent records will not persist", "error", err)
			return
		}
		substrateAgentStateMu.Lock()
		defer substrateAgentStateMu.Unlock()
		substrateStateFile = filepath.Join(dir, "substrate", "agent-records.json")
		loadSubstrateStateLocked()
	})
}

// loadSubstrateStateLocked merges the file into the in-memory maps without
// replacing entries this process already holds. A missing or unreadable file
// leaves the maps as they are. Callers hold substrateAgentStateMu.
func loadSubstrateStateLocked() {
	data, err := os.ReadFile(substrateStateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("substrate: cannot read agent records", "path", substrateStateFile, "error", err)
		}
		return
	}
	var doc substrateStateDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != substrateStateVersion {
		slog.Warn("substrate: ignoring unreadable agent records", "path", substrateStateFile, "version", doc.Version, "error", err)
		return
	}
	for uid, e := range doc.Records {
		if uid == "" || e.Record == nil || e.Record.ID == "" {
			continue
		}
		if _, ok := substrateAgentRecords[uid]; !ok {
			substrateAgentRecords[uid] = e.Record
		}
		if _, ok := substrateControlTokens[e.Record.ID]; !ok && e.ControlToken != "" {
			substrateControlTokens[e.Record.ID] = e.ControlToken
		}
	}
	slog.Info("substrate: loaded agent records", "path", substrateStateFile, "count", len(doc.Records))
}

// saveSubstrateStateLocked writes every record that has an ID to the store.
// A failed write is logged, not returned: the in-memory state is still right
// for this process. Callers hold substrateAgentStateMu.
func saveSubstrateStateLocked() {
	if substrateStateFile == "" {
		return
	}
	doc := substrateStateDoc{Version: substrateStateVersion, Records: make(map[string]substrateStateEntry, len(substrateAgentRecords))}
	for uid, rec := range substrateAgentRecords {
		if rec == nil || rec.ID == "" {
			continue
		}
		doc.Records[uid] = substrateStateEntry{Record: rec, ControlToken: substrateControlTokens[rec.ID]}
	}
	if err := writeSubstrateState(substrateStateFile, doc); err != nil {
		slog.Warn("substrate: cannot persist agent records", "path", substrateStateFile, "error", err)
	}
}

func writeSubstrateState(path string, doc substrateStateDoc) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agent-records-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
