package systemmonitor

import (
	"io"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Octogonapus/S3Benchmark/report"
	"github.com/Octogonapus/S3Benchmark/target"
	"golang.org/x/crypto/ssh"
)

type SystemMonitor interface {
	SetUp() error
	StartMonitoring() error
	StopMonitoring()
	WaitUntilStopped()
	GetSystemMeasurements() *report.SystemMeasurements
}

type systemMonitor struct {
	target     target.Target
	stop       *atomic.Bool
	wg         *sync.WaitGroup
	sm         *report.SystemMeasurements
	s3Prefixes []netip.Prefix
}

func NewSystemMonitor(target target.Target, s3Prefixes []netip.Prefix) SystemMonitor {
	return &systemMonitor{
		target:     target,
		stop:       &atomic.Bool{},
		wg:         &sync.WaitGroup{},
		sm:         &report.SystemMeasurements{},
		s3Prefixes: s3Prefixes,
	}
}

func (mon *systemMonitor) SetUp() error {
	var out []byte
	var err error
	for i := 0; i < 3; i++ {
		out, err = mon.target.RunCommand("apt update -y && apt install -y net-tools")
		if err != nil {
			slog.Debug("failed to install dependencies, will try again", slog.String("command output", string(out)), slog.String("error", err.Error()))
			time.Sleep(30 * time.Second)
		} else {
			break
		}
	}
	if err != nil {
		slog.Error("failed to install dependencies", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return err
	}
	return nil
}

func (mon *systemMonitor) StartMonitoring() error {
	client, err := mon.target.Client()
	if err != nil {
		return err
	}

	mon.wg.Add(1)
	go mon.runMonitor(client)
	return nil
}

func (mon *systemMonitor) StopMonitoring() {
	mon.stop.Store(true)
}

func (mon *systemMonitor) WaitUntilStopped() {
	mon.wg.Wait()
}

func (mon *systemMonitor) GetSystemMeasurements() *report.SystemMeasurements {
	return mon.sm
}

var loopTime = 1 * time.Second
var maxJitter = 1 * time.Second

func (mon *systemMonitor) runMonitor(client *ssh.Client) {
	var prevCPU *cpuTimeStat
	defer mon.wg.Done()
	lastWakeTime := time.Now()
	for {
		// slog.Debug("SystemMonitor: loop", slog.Bool("stop", mon.stop.Load()))
		if mon.stop.Load() {
			break // we deferred wg.Done
		}

		jitterMs := time.Since(lastWakeTime).Milliseconds() - loopTime.Milliseconds()
		if jitterMs > maxJitter.Milliseconds() {
			slog.Warn("SystemMonitor: jitter exceeded maximum", slog.Int64("jitterMs", jitterMs), slog.Int64("maxJitterMs", maxJitter.Milliseconds()))
		}
		lastWakeTime = time.Now()

		buf := mon.runCommand(client, "cat /proc/stat")
		t := time.Now()
		currCPU := parseCPUTimeStat(buf)
		if prevCPU != nil && currCPU != nil {
			mon.appendCPUMetrics(t, currCPU, prevCPU)
		}
		prevCPU = currCPU

		buf = mon.runCommand(client, "cat /proc/diskstats")
		mon.appendDiskIOMetrics(time.Now(), buf)

		buf = mon.runCommand(client, "cat /proc/diskstats")
		mon.appendDiskIOMetrics(time.Now(), buf)

		buf = mon.runCommand(client, "cat /proc/meminfo")
		mon.appendMemoryMetrics(time.Now(), buf)

		buf = mon.runCommand(client, "cat /proc/net/dev")
		mon.appendNetworkMetrics(time.Now(), buf)

		buf = mon.runCommand(client, "netstat -n -4")
		mon.appendS3IPMetrics(time.Now(), buf, mon.s3Prefixes)

		time.Sleep(loopTime)
	}
	slog.Debug("SystemMonitor: stopped")
}

func (mon *systemMonitor) runCommand(client *ssh.Client, cmd string) []byte {
	session, err := client.NewSession()
	if err == io.EOF {
		// TODO try to handle this more gracefully when we purposefully terminate an instance
		slog.Error("SystemMonitor: client got EOF when creating session, stopping monitor because connection is dead", slog.String("error", err.Error()))
		mon.StopMonitoring()
		return nil
	} else if err != nil {
		slog.Warn("SystemMonitor: failed to create session", slog.String("error", err.Error()))
		return nil
	} else {
		defer session.Close()
		buf, err := session.CombinedOutput(cmd)
		if err != nil {
			slog.Warn("SystemMonitor: failed to run command", slog.String("command", cmd), slog.String("output", string(buf)))
			return nil
		} else {
			return buf
		}
	}
}
