// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !nouncore
// +build !nouncore

package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/promslog"
)

func writeUncoreFiles(t *testing.T, sys, dir string, files map[string]string) {
	t.Helper()
	p := filepath.Join(sys, "devices/system/cpu/intel_uncore_frequency", dir)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(p, name), []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newUncoreTestCollector(t *testing.T, sys string) Collector {
	t.Helper()
	config := &NodeCollectorConfig{}
	config.Path.SysPath = &sys
	c, err := NewUncoreCollector(config, promslog.NewNopLogger())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUncoreLegacyAndDomainLayouts(t *testing.T) {
	sys := t.TempDir()
	writeUncoreFiles(t, sys, "package_00_die_00", map[string]string{
		"min_freq_khz": "800000", "max_freq_khz": "2200000", "current_freq_khz": "1200000",
	})
	// current_freq_khz is omitted, as on kernels or permissions that hide it.
	writeUncoreFiles(t, sys, "package_01_die_00", map[string]string{
		"min_freq_khz": "800000", "max_freq_khz": "2400000",
	})
	writeUncoreFiles(t, sys, "uncore00", map[string]string{
		"package_id": "2", "domain_id": "3",
		"min_freq_khz": "700000", "max_freq_khz": "2000000", "current_freq_khz": "900000",
		"initial_min_freq_khz": "600000", "initial_max_freq_khz": "2100000",
	})

	want := map[string]float64{
		"node_uncore_frequency_hertz{domain=0,package=0}":             1.2e9,
		"node_uncore_frequency_hertz{domain=3,package=2}":             9e8,
		"node_uncore_frequency_initial_max_hertz{domain=3,package=2}": 2.1e9,
		"node_uncore_frequency_initial_min_hertz{domain=3,package=2}": 6e8,
		"node_uncore_frequency_max_hertz{domain=0,package=0}":         2.2e9,
		"node_uncore_frequency_max_hertz{domain=0,package=1}":         2.4e9,
		"node_uncore_frequency_max_hertz{domain=3,package=2}":         2e9,
		"node_uncore_frequency_min_hertz{domain=0,package=0}":         8e8,
		"node_uncore_frequency_min_hertz{domain=0,package=1}":         8e8,
		"node_uncore_frequency_min_hertz{domain=3,package=2}":         7e8,
	}

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(collectorShim{newUncoreTestCollector(t, sys)})
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			var labels []string
			for _, l := range m.GetLabel() {
				labels = append(labels, l.GetName()+"="+l.GetValue())
			}
			got[mf.GetName()+"{"+strings.Join(labels, ",")+"}"] = m.GetGauge().GetValue()
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d series, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s: got %v (present=%v), want %v", k, g, ok, v)
		}
	}
}

func TestUncoreNoData(t *testing.T) {
	c := newUncoreTestCollector(t, t.TempDir())
	ch := make(chan prometheus.Metric, 8)
	if err := c.Update(ch); err != ErrNoData {
		t.Fatalf("expected ErrNoData, got %v", err)
	}
}

// collectorShim adapts a Collector to a prometheus.Collector for testutil.
type collectorShim struct{ c Collector }

func (s collectorShim) Describe(chan<- *prometheus.Desc) {}
func (s collectorShim) Collect(ch chan<- prometheus.Metric) {
	if err := s.c.Update(ch); err != nil {
		panic(err)
	}
}
