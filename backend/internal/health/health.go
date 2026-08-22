// Package health computes a sanitized CPU/RAM/Disk summary for Telegram
// notifications. It intentionally reports only aggregate percentages —
// never hostnames, instance labels, or any other server-identifying
// information — since these summaries go to a Telegram chat.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"aruzor/internal/prometheus"
)

const (
	cpuQuery    = `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`
	memoryQuery = `(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100`
	diskQuery   = `(1 - (node_filesystem_avail_bytes{fstype!="tmpfs"} / node_filesystem_size_bytes{fstype!="tmpfs"})) * 100`
)

type Metric struct {
	Label   string
	Percent float64
	OK      bool // whether the value could be read
}

type Summary struct {
	CPU         Metric
	Memory      Metric
	Disk        Metric
	GeneratedAt time.Time
}

func Compute(ctx context.Context, prom *prometheus.Client) Summary {
	return Summary{
		CPU:         fetchPercent(ctx, prom, "CPU", cpuQuery),
		Memory:      fetchPercent(ctx, prom, "Bellek", memoryQuery),
		Disk:        fetchPercent(ctx, prom, "Disk", diskQuery),
		GeneratedAt: time.Now(),
	}
}

func fetchPercent(ctx context.Context, prom *prometheus.Client, label, promQL string) Metric {
	data, err := prom.Query(ctx, promQL, time.Now())
	if err != nil {
		return Metric{Label: label, OK: false}
	}

	var parsed struct {
		Result []struct {
			Value []json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Result) == 0 || len(parsed.Result[0].Value) < 2 {
		return Metric{Label: label, OK: false}
	}

	var raw string
	if err := json.Unmarshal(parsed.Result[0].Value[1], &raw); err != nil {
		return Metric{Label: label, OK: false}
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return Metric{Label: label, OK: false}
	}
	return Metric{Label: label, Percent: value, OK: true}
}

func statusEmoji(percent float64) string {
	switch {
	case percent >= 90:
		return "🔴"
	case percent >= 75:
		return "🟡"
	default:
		return "🟢"
	}
}

// Line renders a single metric the same sanitized way the full summary
// does — aggregate percentage only, never a hostname/instance label.
func (m Metric) Line() string {
	if !m.OK {
		return fmt.Sprintf("⚪ %s: veri yok", m.Label)
	}
	return fmt.Sprintf("%s %s: %%%.0f", statusEmoji(m.Percent), m.Label, m.Percent)
}

// AnyCritical reports whether any metric has crossed the critical (red)
// threshold, used to decide whether an out-of-schedule alert is warranted.
func (s Summary) AnyCritical() bool {
	for _, m := range []Metric{s.CPU, s.Memory, s.Disk} {
		if m.OK && m.Percent >= 90 {
			return true
		}
	}
	return false
}

// Text renders the summary as a Telegram message body. It reports only
// aggregate health, never any server-identifying details.
func (s Summary) Text(title string) string {
	return fmt.Sprintf(
		"%s\n\n%s\n%s\n%s\n\n🕒 %s",
		title,
		s.CPU.Line(),
		s.Memory.Line(),
		s.Disk.Line(),
		s.GeneratedAt.Format("02.01.2006 15:04"),
	)
}
