package apigatling

import (
	"math"
	"sort"
	"sync"
	"time"
)

type Report struct {
	Success            uint64
	ClientErrors       uint64
	ServerErrors       uint64
	NetworkErrors      uint64
	OtherCodes         uint64
	TotalSent          uint64
	TotalDone          uint64
	TotalExecutionTime time.Duration
	RequestsPerSecond  float64
	AverageLatency     time.Duration
	MinLatency         time.Duration
	MaxLatency         time.Duration
	P99Latency         time.Duration
}

type latencyCollector struct {
	mu        sync.Mutex
	durations []time.Duration
}

func newLatencyCollector(capacity int) *latencyCollector {
	if capacity < 0 {
		capacity = 0
	}
	return &latencyCollector{durations: make([]time.Duration, 0, capacity)}
}

func (lc *latencyCollector) add(d time.Duration) {
	lc.mu.Lock()
	lc.durations = append(lc.durations, d)
	lc.mu.Unlock()
}

func (lc *latencyCollector) snapshot() []time.Duration {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	out := make([]time.Duration, len(lc.durations))
	copy(out, lc.durations)
	return out
}

func computeLatencyStats(durations []time.Duration) (avg, min, max, p99 time.Duration) {
	if len(durations) == 0 {
		return 0, 0, 0, 0
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	min = durations[0]
	max = durations[len(durations)-1]

	var total int64
	for _, d := range durations {
		total += int64(d)
	}
	avg = time.Duration(total / int64(len(durations)))

	p99Index := int(math.Ceil(0.99*float64(len(durations)))) - 1
	if p99Index < 0 {
		p99Index = 0
	}
	if p99Index >= len(durations) {
		p99Index = len(durations) - 1
	}
	p99 = durations[p99Index]

	return avg, min, max, p99
}
