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

import "testing"

func TestSubstrateLimits(t *testing.T) {
	tests := []struct {
		name, cpu, memory string
		wantCPU, wantMem  int64
	}{
		{"both unset", "", "", 2000, 2 << 30},
		{"cpu only", "500m", "", 500, 2 << 30},
		{"memory only", "", "4Gi", 2000, 4 << 30},
		{"both set", "1", "512Mi", 1000, 512 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substrateLimits(tt.cpu, tt.memory)
			if err != nil {
				t.Fatal(err)
			}
			if got.GetCpuMillicores() != tt.wantCPU || got.GetMemoryBytes() != tt.wantMem {
				t.Errorf("substrateLimits(%q, %q) = %d millicores, %d bytes; want %d, %d",
					tt.cpu, tt.memory, got.GetCpuMillicores(), got.GetMemoryBytes(), tt.wantCPU, tt.wantMem)
			}
		})
	}
	if _, err := substrateLimits("lots", ""); err == nil {
		t.Error("substrateLimits accepted an invalid cpu quantity")
	}
}
