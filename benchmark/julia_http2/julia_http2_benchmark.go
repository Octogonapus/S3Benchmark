package julia_http2

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

//go:embed julia_http2_benchmark/*
var project embed.FS

type bmark struct {
	input       *JuliaHttp2BenchmarkInput
	ctx         *benchmark.BenchmarkContext
	objectsPath string
	juliaCmd    string
}

type JuliaHttp2BenchmarkInput struct {
	Name          string
	JuliaVersion  string
	Nthreads      int
	NthreadsCmd   string
	SetUpForVTune bool
}

type input struct {
	Bucket      string
	ObjectsPath string
}

type output struct {
	DtMs float64
}

func init() {
	benchmark.RegisterBenchmark("julia_http2", func(a map[string]any) (benchmark.Benchmark, error) {
		input := &JuliaHttp2BenchmarkInput{}
		err := mapstructure.Decode(a, input)
		if err != nil {
			return nil, fmt.Errorf("can't convert input to JuliaHttp2BenchmarkInput: %w", err)
		}
		return NewJuliaHttp2Benchmark(input)
	})
}

func NewJuliaHttp2Benchmark(input *JuliaHttp2BenchmarkInput) (benchmark.Benchmark, error) {
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

	entries, err := project.ReadDir("julia_http2_benchmark")
	if err != nil {
		slog.Error("failed to open the embedded julia project", slog.String("error", err.Error()))
		return err
	}
	for _, entry := range entries {
		var f fs.File
		f, err = project.Open(path.Join("julia_http2_benchmark", entry.Name()))
		if err != nil {
			break
		}
		err = b.ctx.Target.CopyFileTo(f, path.Join("julia_http2_benchmark", entry.Name()))
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

	out, err = b.ctx.Target.RunCommand(fmt.Sprintf("%s -t auto --project=julia_http2_benchmark -e 'using Pkg; Pkg.instantiate(); Pkg.build(); Pkg.precompile()'", b.juliaCmd))
	if err != nil {
		slog.Error("failed to instantiate the julia project", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return err
	}

	return nil
}

func (b *bmark) GetCommand() (string, error) {
	input := input{
		Bucket:      b.ctx.Bucket,
		ObjectsPath: b.objectsPath,
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
		"%s -t %s --project=julia_http2_benchmark -- julia_http2_benchmark/main.jl '%s'",
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
	}
	return &benchOutput, nil
}

func (b *bmark) GetName() string {
	return b.input.Name
}

func (b *bmark) GetInput() map[string]any {
	return util.StructMap(b.input)
}
