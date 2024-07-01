package benchmark

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path"

	"github.com/Octogonapus/S3Benchmark/profile"
	"github.com/Octogonapus/S3Benchmark/report"
	systemmonitor "github.com/Octogonapus/S3Benchmark/system_monitor"
)

type benchmarkRunner struct {
	b              Benchmark
	sm             systemmonitor.SystemMonitor
	prof           profile.Profiler
	ctx            *BenchmarkContext
	profilerKind   profile.ProfilerKind
	profileSaveDir string
	runs           int
}

// Helps implement a benchmark orchestrator. Handles the system monitor and profiler.
// Wrap each benchmark in this interface via NewBenchmarkRunner.
type BenchmarkRunner interface {
	// Set up the benchmark and supporting machinery (e.g. system monitor, profiler).
	SetUp(ctx *BenchmarkContext, s3Prefixes []netip.Prefix) error

	// Run the benchmark and supporting machinery.
	Run() *report.BenchmarkReport
}

type BenchmarkOutput struct {
	TotalTimeSec float64
	Metadata     []any
	Input        map[string]any
}

func NewBenchmarkRunner(b Benchmark, profilerKind profile.ProfilerKind, profileSaveDir string, runs int) BenchmarkRunner {
	return &benchmarkRunner{b: b, profilerKind: profilerKind, profileSaveDir: profileSaveDir, runs: max(runs, 1)}
}

func (br *benchmarkRunner) SetUp(ctx *BenchmarkContext, s3Prefixes []netip.Prefix) error {
	slog.Info("starting benchmark setup", slog.String("name", br.b.GetName()))
	br.ctx = ctx
	br.sm = systemmonitor.NewSystemMonitor(ctx.Target, s3Prefixes)

	err := br.b.SetUp(ctx)
	if err != nil {
		return fmt.Errorf("setting up benchmark failed: %w", err)
	}

	err = br.sm.SetUp()
	if err != nil {
		return fmt.Errorf("setting up SystemMonitor failed: %w", err)
	}

	if br.profilerKind != profile.None {
		br.prof, err = profile.NewProfiler(br.profilerKind, ctx.Target)
		if err != nil {
			return fmt.Errorf("creating profiler failed: %w", err)
		}

		err = br.prof.SetUp()
		if err != nil {
			return fmt.Errorf("setting up Profiler failed: %w", err)
		}
	}

	slog.Info("finished benchmark setup", slog.String("name", br.b.GetName()))
	return nil
}

func (br *benchmarkRunner) Run() *report.BenchmarkReport {
	slog.Info("starting benchmark", slog.String("name", br.b.GetName()))
	rep := &report.BenchmarkReport{Name: br.b.GetName()}
	rep.Input = br.b.GetInput()

	cmd, err := br.b.GetCommand()
	if err != nil {
		rep.Error = fmt.Errorf("getting benchmark command failed: %w", err).Error()
		return rep
	}
	slog.Debug("benchmark command", slog.String("name", br.b.GetName()), slog.String("command", cmd))

	meta := map[string]string{"command": cmd, "profiler": string(br.profilerKind)}
	rep.Metadata = []any{&meta}

	err = br.sm.StartMonitoring()
	if err != nil {
		rep.Error = fmt.Errorf("starting SystemMonitor failed: %w", err).Error()
		return rep
	}
	defer br.sm.StopMonitoring()

	if br.prof != nil {
		remoteResultPath, err := br.prof.ProfileCommand(cmd)
		if err != nil {
			rep.Error = fmt.Errorf("profiling benchmark failed: %w", err).Error()
			return rep
		}

		localResultPath := path.Join(br.profileSaveDir, br.b.GetName()+"-"+path.Base(remoteResultPath))
		meta["profilingResultPath"] = localResultPath
		localResultFile, err := os.OpenFile(localResultPath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
		if err != nil {
			rep.Error = fmt.Errorf("failed to open local result path for writing: %w", err).Error()
			return rep
		}
		defer localResultFile.Close()
		err = br.ctx.Target.CopyFileFrom(remoteResultPath, localResultFile)
		if err != nil {
			rep.Error = fmt.Errorf("failed to copy profiling result: %w", err).Error()
			return rep
		}
	} else {
		for range br.runs {
			out, err := br.ctx.Target.RunCommand(cmd)
			if err != nil {
				slog.Error("running benchmark command failed", slog.String("name", br.b.GetName()), slog.String("error", err.Error()), slog.String("output", string(out)))
				rep.Error = fmt.Errorf("running benchmark failed: %w", err).Error()
				return rep
			}
			slog.Debug("running benchmark command finished", slog.String("name", br.b.GetName()), slog.String("output", string(out)))

			benchOut, err := br.b.ParseCommandOutput(out)
			if err != nil {
				rep.Error = fmt.Errorf("parsing benchmark output failed: %w", err).Error()
				return rep
			}

			rep.TotalTimeSec = append(rep.TotalTimeSec, benchOut.TotalTimeSec)
			rep.Metadata = append(rep.Metadata, benchOut.Metadata...)
		}
	}

	br.sm.StopMonitoring()
	br.sm.WaitUntilStopped()
	rep.SystemMeasurements = br.sm.GetSystemMeasurements()

	slog.Info("finished benchmark", slog.String("name", br.b.GetName()))
	return rep
}
