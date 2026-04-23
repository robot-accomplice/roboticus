package hostresources

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// Snapshot captures machine-state evidence that makes a benchmark row or RCA
// artifact interpretable. A missing snapshot is a measurement defect; a partial
// snapshot is still useful if it records its own collection errors.
type Snapshot struct {
	CollectedAt          string   `json:"collected_at"`
	CPUPercent           float64  `json:"cpu_percent,omitempty"`
	Load1                float64  `json:"load_1,omitempty"`
	Load5                float64  `json:"load_5,omitempty"`
	Load15               float64  `json:"load_15,omitempty"`
	MemoryTotalBytes     uint64   `json:"memory_total_bytes,omitempty"`
	MemoryAvailableBytes uint64   `json:"memory_available_bytes,omitempty"`
	MemoryUsedBytes      uint64   `json:"memory_used_bytes,omitempty"`
	MemoryUsedPercent    float64  `json:"memory_used_percent,omitempty"`
	SwapTotalBytes       uint64   `json:"swap_total_bytes,omitempty"`
	SwapUsedBytes        uint64   `json:"swap_used_bytes,omitempty"`
	SwapUsedPercent      float64  `json:"swap_used_percent,omitempty"`
	OllamaRSSBytes       uint64   `json:"ollama_rss_bytes,omitempty"`
	RoboticusRSSBytes    uint64   `json:"roboticus_rss_bytes,omitempty"`
	Errors               []string `json:"errors,omitempty"`
}

func (s Snapshot) Empty() bool {
	return s.CPUPercent == 0 &&
		s.Load1 == 0 &&
		s.Load5 == 0 &&
		s.Load15 == 0 &&
		s.MemoryTotalBytes == 0 &&
		s.MemoryAvailableBytes == 0 &&
		s.MemoryUsedBytes == 0 &&
		s.MemoryUsedPercent == 0 &&
		s.SwapTotalBytes == 0 &&
		s.SwapUsedBytes == 0 &&
		s.SwapUsedPercent == 0 &&
		s.OllamaRSSBytes == 0 &&
		s.RoboticusRSSBytes == 0 &&
		len(s.Errors) == 0
}

func Marshal(snapshot *Snapshot) string {
	if snapshot == nil {
		return ""
	}
	buf, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(buf)
}

func FromJSON(raw string) *Snapshot {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil
	}
	return &snapshot
}

var (
	samplerMu sync.RWMutex
	samplerFn = sampleLive
)

func SetSamplerForTests(fn func(context.Context) Snapshot) func() {
	samplerMu.Lock()
	prev := samplerFn
	samplerFn = fn
	samplerMu.Unlock()
	return func() {
		samplerMu.Lock()
		samplerFn = prev
		samplerMu.Unlock()
	}
}

func Sample(ctx context.Context) Snapshot {
	samplerMu.RLock()
	fn := samplerFn
	samplerMu.RUnlock()
	if fn == nil {
		return Snapshot{CollectedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	return fn(ctx)
}

func sampleLive(ctx context.Context) Snapshot {
	sampleCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < 250*time.Millisecond {
			cancel()
			sampleCtx, cancel = context.WithTimeout(context.Background(), remaining)
			defer cancel()
		}
	}

	out := Snapshot{
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}
	recordErr := func(label string, err error) {
		if err == nil {
			return
		}
		out.Errors = append(out.Errors, label+": "+err.Error())
	}

	if pct, err := cpu.PercentWithContext(sampleCtx, 0, false); err != nil {
		recordErr("cpu", err)
	} else if len(pct) > 0 {
		out.CPUPercent = pct[0]
	}

	if avg, err := load.AvgWithContext(sampleCtx); err != nil {
		recordErr("load", err)
	} else {
		out.Load1 = avg.Load1
		out.Load5 = avg.Load5
		out.Load15 = avg.Load15
	}

	if vm, err := mem.VirtualMemoryWithContext(sampleCtx); err != nil {
		recordErr("memory", err)
	} else {
		out.MemoryTotalBytes = vm.Total
		out.MemoryAvailableBytes = vm.Available
		out.MemoryUsedBytes = vm.Used
		out.MemoryUsedPercent = vm.UsedPercent
	}

	if sm, err := mem.SwapMemoryWithContext(sampleCtx); err != nil {
		recordErr("swap", err)
	} else {
		out.SwapTotalBytes = sm.Total
		out.SwapUsedBytes = sm.Used
		out.SwapUsedPercent = sm.UsedPercent
	}

	if current, err := process.NewProcessWithContext(sampleCtx, int32(os.Getpid())); err != nil {
		recordErr("roboticus_process", err)
	} else if info, err := current.MemoryInfoWithContext(sampleCtx); err != nil {
		recordErr("roboticus_rss", err)
	} else if info != nil {
		out.RoboticusRSSBytes = info.RSS
	}

	if procs, err := process.ProcessesWithContext(sampleCtx); err != nil {
		recordErr("processes", err)
	} else {
		var ollamaRSS uint64
		for _, proc := range procs {
			name, err := proc.NameWithContext(sampleCtx)
			if err != nil {
				continue
			}
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "ollama" && !strings.Contains(name, "ollama") {
				continue
			}
			if info, err := proc.MemoryInfoWithContext(sampleCtx); err == nil && info != nil {
				ollamaRSS += info.RSS
			}
		}
		out.OllamaRSSBytes = ollamaRSS
	}

	return out
}
