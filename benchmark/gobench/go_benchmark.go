package gobench

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
	"github.com/mitchellh/mapstructure"
)

//go:embed go_benchmark/*
var goProject embed.FS

type bmark struct {
	input       *GoBenchmarkInput
	ctx         *benchmark.BenchmarkContext
	objectsPath string
}

type GoBenchmarkInput struct {
	Name                string
	DownloadInParts     bool
	DownloadConcurrency int
	PartSize            int
	PartConcurrency     int
	Repeats             int
}

type input struct {
	Bucket              string
	ObjectsPath         string
	DownloadStrategy    string
	DownloadConcurrency int
	PartSize            int
	PartConcurrency     int
	Repeats             int
}

type output struct {
	TotalTimeSec float64
}

func init() {
	benchmark.RegisterBenchmark("go", func(a map[string]any) (benchmark.Benchmark, error) {
		input := &GoBenchmarkInput{}
		err := mapstructure.Decode(a, input)
		if err != nil {
			return nil, fmt.Errorf("can't convert input to GoBenchmarkInput: %w", err)
		}
		return NewGoBenchmark(input)
	})
}

func NewGoBenchmark(input *GoBenchmarkInput) (benchmark.Benchmark, error) {
	return &bmark{input: input}, nil
}

func (b *bmark) installGo() error {
	var out []byte
	var err error
	for i := 0; i < 3; i++ {
		out, err = b.ctx.Target.RunCommand("wget -nv https://go.dev/dl/go1.22.4.linux-amd64.tar.gz && rm -rf /usr/local/go && tar -C /usr/local -xzf go1.22.4.linux-amd64.tar.gz")
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

func (b *bmark) SetUp(ctx *benchmark.BenchmarkContext) error {
	b.ctx = ctx

	err := b.installGo()
	if err != nil {
		return err
	}

	entries, err := goProject.ReadDir("go_benchmark")
	if err != nil {
		slog.Error("failed to open the embedded go project", slog.String("error", err.Error()))
		return err
	}
	for _, entry := range entries {
		var f fs.File
		f, err = goProject.Open(path.Join("go_benchmark", entry.Name()))
		if err != nil {
			break
		}
		remoteName := entry.Name()
		remoteName = strings.TrimSuffix(remoteName, ".txt") // go mod files must have .txt to allow embedding so remove it here
		err = b.ctx.Target.CopyFileTo(f, path.Join("go_benchmark", remoteName))
		if err != nil {
			break
		}
	}
	if err != nil {
		slog.Error("failed to copy the go project", slog.String("error", err.Error()))
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

	out, err := b.ctx.Target.RunCommand("cd go_benchmark && /usr/local/go/bin/go build")
	if err != nil {
		slog.Error("failed to build the go project", slog.String("error", err.Error()), slog.String("command output", string(out)))
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

	downloadStrategy := fmt.Sprintf("%s", parts)

	input := input{
		Bucket:              b.ctx.Bucket,
		ObjectsPath:         b.objectsPath,
		DownloadStrategy:    downloadStrategy,
		DownloadConcurrency: b.input.DownloadConcurrency,
		PartSize:            b.input.PartSize,
		PartConcurrency:     b.input.PartConcurrency,
		Repeats:             max(1, b.input.Repeats),
	}
	buf, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"go_benchmark/go_benchmark '%s'",
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
		TotalTimeSec: output.TotalTimeSec,
	}
	return &benchOutput, nil
}

func (b *bmark) GetName() string {
	return b.input.Name
}

func (b *bmark) GetInput() map[string]any {
	return util.StructMap(b.input)
}
