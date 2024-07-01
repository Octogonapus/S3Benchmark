package systemmonitor

import (
	"strconv"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/report"
)

type cpuTimeStat struct {
	user      int
	system    int
	idle      int
	nice      int
	iowait    int
	irq       int
	softIrq   int
	steal     int
	guest     int
	guestNice int
}

func (ts *cpuTimeStat) totalCPUTime() int {
	return ts.user + ts.system + ts.nice + ts.iowait + ts.irq + ts.softIrq + ts.steal + ts.idle
}

func parseCPUTimeStat(buf []byte) *cpuTimeStat {
	for _, line := range strings.Split(string(buf), "\n") {
		// We only want to total CPU usage, ignore per-core metrics and other metrics
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}

		// The rest of this loop only runs one time for the one total CPU usage line
		parts := strings.Fields(line)
		User, _ := strconv.Atoi(parts[1])
		Nice, _ := strconv.Atoi(parts[2])
		System, _ := strconv.Atoi(parts[3])
		Idle, _ := strconv.Atoi(parts[4])
		Iowait, _ := strconv.Atoi(parts[5])
		Irq, _ := strconv.Atoi(parts[6])
		SoftIrq, _ := strconv.Atoi(parts[7])
		Steal, _ := strconv.Atoi(parts[8])
		Guest, _ := strconv.Atoi(parts[9])
		GuestNice, _ := strconv.Atoi(parts[10])
		return &cpuTimeStat{
			user:      User,
			nice:      Nice,
			system:    System,
			idle:      Idle,
			iowait:    Iowait,
			irq:       Irq,
			softIrq:   SoftIrq,
			steal:     Steal,
			guest:     Guest,
			guestNice: GuestNice,
		}
	}
	return nil
}

func (mon *systemMonitor) appendCPUMetrics(now time.Time, curr *cpuTimeStat, prev *cpuTimeStat) {
	delta := float64(curr.totalCPUTime() - prev.totalCPUTime())
	if delta < 0 {
		return
	}
	mon.sm.CpuUsageUser = append(mon.sm.CpuUsageUser, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.user-prev.user-(curr.guest-prev.guest))) / delta,
	})
	mon.sm.CpuUsageSystem = append(mon.sm.CpuUsageSystem, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.system-prev.system)) / delta,
	})
	mon.sm.CpuUsageIdle = append(mon.sm.CpuUsageIdle, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.idle-prev.idle)) / delta,
	})
	mon.sm.CpuUsageNice = append(mon.sm.CpuUsageNice, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.nice-prev.nice-(curr.guestNice-prev.guestNice))) / delta,
	})
	mon.sm.CpuUsageIowait = append(mon.sm.CpuUsageIowait, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.iowait-prev.iowait)) / delta,
	})
	mon.sm.CpuUsageIrq = append(mon.sm.CpuUsageIrq, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.irq-prev.irq)) / delta,
	})
	mon.sm.CpuUsageSoftIrq = append(mon.sm.CpuUsageSoftIrq, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.softIrq-prev.softIrq)) / delta,
	})
	mon.sm.CpuUsageSteal = append(mon.sm.CpuUsageSteal, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.steal-prev.steal)) / delta,
	})
	mon.sm.CpuUsageGuest = append(mon.sm.CpuUsageGuest, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.guest-prev.guest)) / delta,
	})
	mon.sm.CpuUsageGuestNice = append(mon.sm.CpuUsageGuestNice, report.Measurement[float64]{
		Time:  now.Unix(),
		Value: float64(100*(curr.guestNice-prev.guestNice)) / delta,
	})
}
