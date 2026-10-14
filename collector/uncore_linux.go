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
	"fmt"
	"log/slog"
	"regexp"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/procfs/sysfs"
)

const uncoreCollectorSubsystem = "uncore"

var (
	uncoreLegacyNameRE = regexp.MustCompile(`^package_(\d+)_die_(\d+)$`)
	uncoreDomainNameRE = regexp.MustCompile(`^uncore(\d+)$`)

	uncoreLabels = []string{"package", "domain"}

	uncoreFreqHertzDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, uncoreCollectorSubsystem, "frequency_hertz"),
		"Current Intel uncore frequency in hertz.",
		uncoreLabels, nil,
	)
	uncoreFreqMinHertzDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, uncoreCollectorSubsystem, "frequency_min_hertz"),
		"Minimum Intel uncore frequency limit in hertz.",
		uncoreLabels, nil,
	)
	uncoreFreqMaxHertzDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, uncoreCollectorSubsystem, "frequency_max_hertz"),
		"Maximum Intel uncore frequency limit in hertz.",
		uncoreLabels, nil,
	)
	uncoreFreqInitialMinHertzDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, uncoreCollectorSubsystem, "frequency_initial_min_hertz"),
		"Minimum Intel uncore frequency limit at boot in hertz.",
		uncoreLabels, nil,
	)
	uncoreFreqInitialMaxHertzDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, uncoreCollectorSubsystem, "frequency_initial_max_hertz"),
		"Maximum Intel uncore frequency limit at boot in hertz.",
		uncoreLabels, nil,
	)
)

type uncoreCollector struct {
	fs     sysfs.FS
	logger *slog.Logger
}

func init() {
	registerCollector(uncoreCollectorSubsystem, defaultDisabled, NewUncoreCollector)
}

// NewUncoreCollector returns a new Collector exposing Intel uncore frequency
// statistics from /sys/devices/system/cpu/intel_uncore_frequency.
func NewUncoreCollector(config *NodeCollectorConfig, logger *slog.Logger) (Collector, error) {
	fs, err := sysfs.NewFS(*config.Path.SysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sysfs: %w", err)
	}

	return &uncoreCollector{
		fs:     fs,
		logger: logger,
	}, nil
}

// Update implements Collector and exposes Intel uncore frequency metrics.
func (c *uncoreCollector) Update(ch chan<- prometheus.Metric) error {
	domains, err := c.fs.SystemUncoreFrequency()
	if err != nil {
		return fmt.Errorf("failed to retrieve uncore frequency stats: %w", err)
	}
	if len(domains) == 0 {
		c.logger.Debug("Platform doesn't have intel_uncore_frequency files present")
		return ErrNoData
	}

	// sysfs uncore frequency values are kHz, thus multiply by 1000 to export
	// base units (hz).
	for _, d := range domains {
		pkg, domain := uncoreLabelValues(d)
		for _, m := range []struct {
			desc  *prometheus.Desc
			value *uint64
		}{
			{uncoreFreqHertzDesc, d.CurrentFreqKhz},
			{uncoreFreqMinHertzDesc, d.MinFreqKhz},
			{uncoreFreqMaxHertzDesc, d.MaxFreqKhz},
			{uncoreFreqInitialMinHertzDesc, d.InitialMinFreqKhz},
			{uncoreFreqInitialMaxHertzDesc, d.InitialMaxFreqKhz},
		} {
			if m.value == nil {
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				m.desc,
				prometheus.GaugeValue,
				float64(*m.value)*1000.0,
				pkg, domain,
			)
		}
	}
	return nil
}

// uncoreLabelValues returns the package and domain label values for a domain.
// For the legacy package_NN_die_MM layout, the domain is the die number. For
// the uncoreNN layout the package_id and domain_id files are used, falling
// back to the directory number for the domain and an empty package.
func uncoreLabelValues(d sysfs.SystemUncoreFrequency) (pkg, domain string) {
	if m := uncoreLegacyNameRE.FindStringSubmatch(d.Name); m != nil {
		return trimLeadingZeros(m[1]), trimLeadingZeros(m[2])
	}
	if d.PackageID != nil {
		pkg = strconv.FormatUint(*d.PackageID, 10)
	}
	if d.DomainID != nil {
		domain = strconv.FormatUint(*d.DomainID, 10)
	} else if m := uncoreDomainNameRE.FindStringSubmatch(d.Name); m != nil {
		domain = trimLeadingZeros(m[1])
	}
	return pkg, domain
}

// trimLeadingZeros turns "00" into "0" and "07" into "7".
func trimLeadingZeros(s string) string {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return s
	}
	return strconv.FormatUint(n, 10)
}
