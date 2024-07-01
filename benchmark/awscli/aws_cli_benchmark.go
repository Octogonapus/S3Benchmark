package awscli

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/benchmark"
	"github.com/Octogonapus/S3Benchmark/util"
	"github.com/mitchellh/mapstructure"
)

type AwsCliBenchmarkInput struct {
	Name string
}

type bmark struct {
	ctx  *benchmark.BenchmarkContext
	name string
}

func init() {
	benchmark.RegisterBenchmark("aws_cli", func(a map[string]any) (benchmark.Benchmark, error) {
		input := &AwsCliBenchmarkInput{}
		err := mapstructure.Decode(a, input)
		if err != nil {
			return nil, fmt.Errorf("can't convert input to AwsCliBenchmarkInput: %w", err)
		}
		return NewAwsCliBenchmark(input), nil
	})
}

func NewAwsCliBenchmark(input *AwsCliBenchmarkInput) benchmark.Benchmark {
	return &bmark{name: input.Name}
}

func (b *bmark) GetCommand() (string, error) {
	// TODO we should really be copying the objects and not the entire bucket here but for now it is okay
	return fmt.Sprintf("time aws s3 cp --recursive %s %s", fmt.Sprintf("s3://%s", b.ctx.Bucket), b.ctx.Bucket), nil
}

func (b *bmark) ParseCommandOutput(out []byte) (*benchmark.BenchmarkOutput, error) {
	line := util.LastNonEmptyLine(out)
	slog.Debug("selected benchmark output", slog.String("line", line))

	parts := strings.Fields(line)
	if len(parts) < 9 {
		return nil, fmt.Errorf("did not find time in command output")
	}

	totalTime, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse total time: %w", err)
	}

	return &benchmark.BenchmarkOutput{TotalTimeSec: float64(totalTime)}, nil
}

func (b *bmark) SetUp(ctx *benchmark.BenchmarkContext) error {
	b.ctx = ctx

	var out []byte
	var err error
	for i := 0; i < 3; i++ {
		out, err = b.ctx.Target.RunCommand("apt update -y && apt install -y unzip net-tools")
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

	out, err = b.ctx.Target.RunCommand("curl https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o awscliv2.zip")
	if err != nil {
		slog.Error("failed to download the AWS CLI", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return err
	}

	out, err = b.ctx.Target.RunCommand("unzip awscliv2.zip")
	if err != nil {
		slog.Error("failed to unzip the AWS CLI", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return err
	}

	out, err = b.ctx.Target.RunCommand("./aws/install")
	if err != nil {
		slog.Error("failed to install the AWS CLI", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return err
	}

	return nil
}

func (b *bmark) GetName() string {
	return b.name
}

func (b *bmark) GetInput() map[string]any {
	return map[string]any{}
}
