package julia_awsjl

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/benchmark"
	"github.com/Octogonapus/S3Benchmark/util"
	"github.com/hashicorp/go-version"
	"github.com/mitchellh/mapstructure"
)

//go:embed julia_awsjl_benchmark/*
var juliaProject embed.FS

type JuliaAwsjlBackend string

const (
	HTTP      JuliaAwsjlBackend = "http"
	Downloads JuliaAwsjlBackend = "downloads"
)

type JuliaDownloadStrategy string

const (
	DynamicThreads JuliaDownloadStrategy = "dynamic threads"
	GreedyThreads  JuliaDownloadStrategy = "greedy threads"
	KeepOffThread1 JuliaDownloadStrategy = "keep off thread 1"
)

type bmark struct {
	input       *JuliaAwsjlBenchmarkInput
	ctx         *benchmark.BenchmarkContext
	objectsPath string
	juliaCmd    string
}

type JuliaAwsjlBenchmarkInput struct {
	Name                  string
	JuliaVersion          string
	Nthreads              int
	NthreadsCmd           string
	AwsBackend            JuliaAwsjlBackend
	WriteToDisk           bool
	DownloadStrategy      JuliaDownloadStrategy
	DownloadInParts       bool
	DownloadPartSizeBytes int
	DownloadPartsNThreads int
	ProfileUsingBuiltin   bool
	SetUpForVTune         bool
}

type input struct {
	Backend               string
	Bucket                string
	ObjectsPath           string
	WriteToDisk           bool
	DownloadStrategy      string
	DownloadPartSizeBytes int
	DownloadPartsNThreads int
	ShouldProfile         bool
}

type output struct {
	DtMs    float64
	Profile string
}

func init() {
	benchmark.RegisterBenchmark("julia_awsjl", func(a map[string]any) (benchmark.Benchmark, error) {
		input := &JuliaAwsjlBenchmarkInput{}
		err := mapstructure.Decode(a, input)
		if err != nil {
			return nil, fmt.Errorf("can't convert input to JuliaAwsjlBenchmarkInput: %w", err)
		}
		return NewJuliaAwsjlBenchmark(input)
	})
}

func NewJuliaAwsjlBenchmark(input *JuliaAwsjlBenchmarkInput) (benchmark.Benchmark, error) {
	if input.ProfileUsingBuiltin && input.SetUpForVTune {
		return nil, fmt.Errorf("the julia benchmark should not use both built-in profiling and vtune")
	}
	if input.DownloadStrategy == GreedyThreads && !input.DownloadInParts {
		return nil, fmt.Errorf("if using greedy schedule, must also use download in parts")
	}
	if input.DownloadStrategy == KeepOffThread1 && input.DownloadInParts {
		return nil, fmt.Errorf("can't use KeepOffThread1 strategy and download in parts")
	}
	if input.Nthreads != 0 && len(input.NthreadsCmd) > 0 {
		return nil, fmt.Errorf("can't set both Nthreads and NthreadsCmd")
	}
	if strings.HasPrefix(input.JuliaVersion, "v") {
		return nil, fmt.Errorf("julia version string must not have a v prefix")
	}
	juliaVersion, err := version.NewVersion(input.JuliaVersion)
	if err != nil {
		return nil, fmt.Errorf("can't parse julia version: %w", err)
	}
	juliaMinorVersion := juliaVersion.Segments()[1]
	if input.DownloadStrategy == GreedyThreads && juliaMinorVersion < 11 {
		return nil, fmt.Errorf("if using greedy schedule, julia version must be at least 1.11")
	}
	if juliaMinorVersion < 10 {
		slog.Warn("The Julia benchmark isn't intended to be ran on versions earlier than 1.10")
	}
	return &bmark{input: input}, nil
}

func (b *bmark) installJulia() error {
	if b.input.SetUpForVTune {
		var out []byte
		var err error
		for i := 0; i < 3; i++ {
			out, err = b.ctx.Target.RunCommand("apt-get update -y && apt-get install -y build-essential libatomic1 python3 gfortran perl wget m4 cmake pkg-config curl")
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

		out, err = b.ctx.Target.RunCommand(
			fmt.Sprintf(
				"git clone https://github.com/JuliaLang/julia.git && cd julia && git checkout v%s && echo USE_INTEL_JITEVENTS=1 > Make.user && make -j$(nproc)",
				b.input.JuliaVersion,
			),
		)
		if err != nil {
			slog.Error("failed to build custom julia", slog.String("command output", string(out)), slog.String("error", err.Error()))
			return err
		}

		out, err = b.ctx.Target.RunCommand("echo '#!/bin/bash\nENABLE_JITPROFILING=1 ~/julia/usr/bin/julia -q -g2 -O0 \"$@\"' > runjulia.sh && chmod +x runjulia.sh")
		if err != nil {
			slog.Error("failed to create custom runjulia script", slog.String("command output", string(out)), slog.String("error", err.Error()))
			return err
		}

		b.juliaCmd = "~/runjulia.sh"
	} else {
		var out []byte
		var err error
		for i := 0; i < 3; i++ {
			out, err = b.ctx.Target.RunCommand(fmt.Sprintf("curl -fsSL https://install.julialang.org | sh -s -- -y --default-channel %s", b.input.JuliaVersion))
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

		b.juliaCmd = "~/.juliaup/bin/julia -q"
	}
	return nil
}

func (b *bmark) SetUp(ctx *benchmark.BenchmarkContext) error {
	b.ctx = ctx

	var out []byte
	var err error
	err = b.installJulia()
	if err != nil {
		return err
	}

	entries, err := juliaProject.ReadDir("julia_awsjl_benchmark")
	if err != nil {
		slog.Error("failed to open the embedded julia project", slog.String("error", err.Error()))
		return err
	}
	for _, entry := range entries {
		var f fs.File
		f, err = juliaProject.Open(path.Join("julia_awsjl_benchmark", entry.Name()))
		if err != nil {
			break
		}
		err = b.ctx.Target.CopyFileTo(f, path.Join("julia_awsjl_benchmark", entry.Name()))
		if err != nil {
			break
		}
	}
	if err != nil {
		slog.Error("failed to copy the julia project", slog.String("error", err.Error()))
		return err
	}

	buf, err := json.Marshal(b.ctx.Keys)
	if err != nil {
		return err
	}
	b.objectsPath = fmt.Sprintf("./%s.json", util.Randstring(8))
	err = b.ctx.Target.CopyFileTo(bytes.NewReader(buf), b.objectsPath)
	if err != nil {
		return err
	}

	out, err = b.ctx.Target.RunCommand(fmt.Sprintf("%s -t auto --project=julia_awsjl_benchmark -e 'using Pkg; Pkg.instantiate(); Pkg.build(); Pkg.precompile()'", b.juliaCmd))
	if err != nil {
		slog.Error("failed to instantiate the julia project", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return err
	}

	return nil
}

func (b *bmark) GetCommand() (string, error) {
	var parts string
	if b.input.DownloadInParts {
		parts = "parts"
	} else {
		parts = "no parts"
	}

	downloadStrategy := fmt.Sprintf("%s, %s", string(b.input.DownloadStrategy), parts)

	input := input{
		Backend:               string(b.input.AwsBackend),
		Bucket:                b.ctx.Bucket,
		ObjectsPath:           b.objectsPath,
		WriteToDisk:           b.input.WriteToDisk,
		DownloadStrategy:      downloadStrategy,
		DownloadPartSizeBytes: b.input.DownloadPartSizeBytes,
		DownloadPartsNThreads: b.input.DownloadPartsNThreads,
		ShouldProfile:         b.input.ProfileUsingBuiltin,
	}
	buf, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	nthreadsCmd := b.input.NthreadsCmd
	if len(nthreadsCmd) == 0 {
		nthreadsCmd = fmt.Sprintf("%d", b.input.Nthreads)
	}

	return fmt.Sprintf(
		"%s -t %s --project=julia_awsjl_benchmark -- julia_awsjl_benchmark/main.jl '%s'",
		b.juliaCmd,
		nthreadsCmd,
		string(buf),
	), nil
}

func (b *bmark) ParseCommandOutput(out []byte) (*benchmark.BenchmarkOutput, error) {
	line := util.LastNonEmptyLine(out)
	slog.Debug("selected benchmark output", slog.String("line", line))

	output := output{}
	err := json.Unmarshal([]byte(line), &output)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling benchmark output failed: %w", err)
	}

	benchOutput := benchmark.BenchmarkOutput{
		TotalTimeSec: output.DtMs / 1000.0,
		Metadata: []any{
			map[string]string{"juliaprofile": output.Profile},
		},
	}
	return &benchOutput, nil
}

func (b *bmark) GetName() string {
	return b.input.Name
}

func (b *bmark) GetInput() map[string]any {
	return util.StructMap(b.input)
}
