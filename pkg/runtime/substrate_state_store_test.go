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
	"os"
	"path/filepath"
	"testing"
)

// useSubstrateStateFile points the store at a temp file and empties the
// in-memory state, restoring both when the test ends.
func useSubstrateStateFile(t *testing.T) string {
	t.Helper()
	substrateAgentStateMu.Lock()
	prevFile, prevRecords, prevTokens := substrateStateFile, substrateAgentRecords, substrateControlTokens
	substrateStateFile = filepath.Join(t.TempDir(), "substrate", "agent-records.json")
	substrateAgentRecords = map[string]*substrateAgentRecord{}
	substrateControlTokens = map[string]string{}
	path := substrateStateFile
	substrateAgentStateMu.Unlock()
	t.Cleanup(func() {
		substrateAgentStateMu.Lock()
		substrateStateFile, substrateAgentRecords, substrateControlTokens = prevFile, prevRecords, prevTokens
		substrateAgentStateMu.Unlock()
	})
	return path
}

func wipeSubstrateMemory() {
	substrateAgentRecords = map[string]*substrateAgentRecord{}
	substrateControlTokens = map[string]string{}
}

func TestSubstrateStateStore_SurvivesRestart(t *testing.T) {
	path := useSubstrateStateFile(t)

	substrateAgentStateMu.Lock()
	substrateAgentRecords["uid-1"] = &substrateAgentRecord{ID: "space/actor-1", Labels: map[string]string{"scion.name": "a1"}, ProjectID: "p1"}
	substrateControlTokens["space/actor-1"] = "token-1"
	substrateAgentRecords["uid-legacy"] = &substrateAgentRecord{Labels: map[string]string{"scion.name": "old"}}
	saveSubstrateStateLocked()
	wipeSubstrateMemory()
	loadSubstrateStateLocked()
	rec, token := substrateAgentRecords["uid-1"], substrateControlTokens["space/actor-1"]
	_, legacy := substrateAgentRecords["uid-legacy"]
	substrateAgentStateMu.Unlock()

	if rec == nil || rec.ProjectID != "p1" || rec.Labels["scion.name"] != "a1" {
		t.Fatalf("record after reload = %+v", rec)
	}
	if token != "token-1" {
		t.Fatalf("control token after reload = %q, want token-1", token)
	}
	if legacy {
		t.Error("a record without an ID was persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("state file mode = %o, want 600", mode)
	}
}

func TestSubstrateStateStore_DeleteIsPersisted(t *testing.T) {
	useSubstrateStateFile(t)

	substrateAgentStateMu.Lock()
	substrateAgentRecords["uid-1"] = &substrateAgentRecord{ID: "space/actor-1"}
	substrateControlTokens["space/actor-1"] = "token-1"
	saveSubstrateStateLocked()
	delete(substrateAgentRecords, "uid-1")
	delete(substrateControlTokens, "space/actor-1")
	saveSubstrateStateLocked()
	wipeSubstrateMemory()
	loadSubstrateStateLocked()
	n := len(substrateAgentRecords) + len(substrateControlTokens)
	substrateAgentStateMu.Unlock()

	if n != 0 {
		t.Fatalf("deleted agent came back after reload (%d entries)", n)
	}
}

func TestSubstrateStateStore_LoadKeepsLiveEntriesAndIgnoresBadFiles(t *testing.T) {
	path := useSubstrateStateFile(t)

	substrateAgentStateMu.Lock()
	substrateAgentRecords["uid-1"] = &substrateAgentRecord{ID: "space/actor-1", ProjectID: "stored"}
	substrateControlTokens["space/actor-1"] = "stored-token"
	saveSubstrateStateLocked()
	substrateAgentRecords["uid-1"] = &substrateAgentRecord{ID: "space/actor-1", ProjectID: "live"}
	substrateControlTokens["space/actor-1"] = "live-token"
	loadSubstrateStateLocked()
	project, token := substrateAgentRecords["uid-1"].ProjectID, substrateControlTokens["space/actor-1"]
	substrateAgentStateMu.Unlock()
	if project != "live" || token != "live-token" {
		t.Errorf("load replaced live state: project %q, token %q", project, token)
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	substrateAgentStateMu.Lock()
	wipeSubstrateMemory()
	loadSubstrateStateLocked()
	n := len(substrateAgentRecords)
	substrateAgentStateMu.Unlock()
	if n != 0 {
		t.Errorf("corrupt state file produced %d records", n)
	}
}
