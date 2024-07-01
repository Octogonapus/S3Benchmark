package systemmonitor

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/report"
)

func (mon *systemMonitor) appendMemoryMetrics(now time.Time, buf []byte) {
	total := 0
	free := 0
	buffers := 0
	cached := 0
	available := 0
	swapCached := 0
	swapFree := 0
	swapTotal := 0

	for _, line := range strings.Split(string(buf), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		value, _ := strconv.Atoi(parts[1])
		bytes := value * 1024
		switch key := parts[0][:len(parts[0])-1]; key {
		case "MemTotal":
			total = bytes
		case "MemFree":
			free = bytes
		case "MemAvailable":
			available = bytes
		case "Buffers":
			buffers = bytes
		case "Cached":
			cached = bytes
		case "SReclaimable":
			cached += bytes
		case "SwapCached":
			swapCached = bytes
		case "SwapFree":
			swapFree = bytes
		case "SwapTotal":
			swapTotal = bytes
		}
	}

	used := total - free - buffers - cached
	usedPct := 100 * (float64(used) / (float64(total)))
	availablePct := 100 * (float64(available) / (float64(total)))
	swapUsed := swapTotal - swapFree - swapCached
	swapUsedPct := 100 * (float64(swapUsed) / (float64(swapTotal)))
	if math.IsNaN(swapUsedPct) {
		swapUsedPct = 0
	}

	mon.sm.MemUsedBytes = append(mon.sm.MemUsedBytes, report.Measurement[int]{
		Time:  now.Unix(),
		Value: used,
	})
	mon.sm.MemUsedPct = append(mon.sm.MemUsedPct, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: usedPct,
	})
	mon.sm.MemAvailBytes = append(mon.sm.MemAvailBytes, report.Measurement[int]{
		Time:  now.Unix(),
		Value: available,
	})
	mon.sm.MemAvailPct = append(mon.sm.MemAvailPct, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: availablePct,
	})
	mon.sm.SwapUsedBytes = append(mon.sm.SwapUsedBytes, report.Measurement[int]{
		Time:  now.Unix(),
		Value: swapUsed,
	})
	mon.sm.SwapUsedPct = append(mon.sm.SwapUsedPct, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: swapUsedPct,
	})
}
