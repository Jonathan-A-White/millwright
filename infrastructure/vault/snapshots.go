package vault

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// MetricsDir and SnapshotsFile name the factory's key-metric snapshots in the
// vault: one JSON line a day, appended and never rewritten.
const (
	MetricsDir     = "metrics"
	SnapshotsFile  = "factory.jsonl"
	snapshotsLines = 4 * 1024 * 1024
)

var (
	_ application.MetricSnapshots = (*Vault)(nil)
	_ application.BatchMetrics    = (*Vault)(nil)
)

// SnapshotsPath is where the snapshots live in the vault.
func (v *Vault) SnapshotsPath() string {
	return filepath.Join(v.dir, MetricsDir, SnapshotsFile)
}

// Append implements application.MetricSnapshots: one line on the end of the
// file, made when there is none. Only the home writes the daily snapshot, once
// a day, so there is nothing for two hosts to merge.
func (v *Vault) Append(_ context.Context, snapshot application.MetricSnapshot) error {
	return v.appendMetricLine(snapshot)
}

// appendMetricLine writes one JSON line on the end of the file, made when there is none.
func (v *Vault) appendMetricLine(entry any) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := v.SnapshotsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening the key-metric snapshots to append to them: %w", err)
	}
	if ends, err := endsInNewline(path); err == nil && !ends {
		line = append([]byte("\n"), line...)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("appending to the key-metric snapshots: %w", err)
	}
	return file.Close()
}

// Snapshots implements application.MetricSnapshots: the snapshots oldest first;
// none and no error when the file is not there. A line that is not a snapshot
// is skipped rather than hiding the rest.
func (v *Vault) Snapshots(context.Context) ([]application.MetricSnapshot, error) {
	file, err := os.Open(v.SnapshotsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the key-metric snapshots: %w", err)
	}
	defer file.Close()
	var kept []application.MetricSnapshot
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), snapshotsLines)
	for lines.Scan() {
		var one application.MetricSnapshot
		if json.Unmarshal(lines.Bytes(), &one) == nil && one.Day != "" {
			kept = append(kept, one)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading the key-metric snapshots: %w", err)
	}
	return kept, nil
}

// AppendBatch implements application.BatchMetrics: a batch's line on the end of
// the same file. A line is appended whole in one write, and every line is its
// own, so two hosts' lines merge by union.
func (v *Vault) AppendBatch(_ context.Context, metric application.BatchMetric) error {
	metric.Kind = application.BatchKind
	return v.appendMetricLine(metric)
}

// Batches implements application.BatchHistory: the batch lines oldest first as
// the file holds them; none and no error when the file is not there. A line
// that is not a batch's is skipped.
func (v *Vault) Batches(context.Context) ([]application.BatchMetric, error) {
	file, err := os.Open(v.SnapshotsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the batch metrics: %w", err)
	}
	defer file.Close()
	var kept []application.BatchMetric
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), snapshotsLines)
	for lines.Scan() {
		var one application.BatchMetric
		if json.Unmarshal(lines.Bytes(), &one) == nil && one.Kind == application.BatchKind && one.Rig != "" {
			kept = append(kept, one)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading the batch metrics: %w", err)
	}
	return kept, nil
}
