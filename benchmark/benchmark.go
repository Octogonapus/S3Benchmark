package benchmark

import (
	"fmt"

	"github.com/Octogonapus/S3Benchmark/target"
)

type BenchmarkContext struct {
	Target            target.Target
	DesiredThroughput float64
	Bucket            string
	Keys              []string
	Region            string
}

type Benchmark interface {
	// Set up the benchmark. May involve installing software or copying files.
	SetUp(*BenchmarkContext) error

	// Return the command to run the benchmark. If the benchmark needs a warmup period, that must be included in
	// this function (but do not report results from that period).
	GetCommand() (string, error)

	// Parse the entire output from running the benchmark.
	ParseCommandOutput(out []byte) (*BenchmarkOutput, error)

	// A human-friendly name the user can set for this benchmark. Only used for debugging/printing.
	GetName() string

	// Any input given to this benchmark by the user. Included in the benchmark's report. Not used for anything else.
	GetInput() map[string]any
}

type benchmarkType string

type benchmarkFactory func(map[string]any) (Benchmark, error)

var benchmarks map[benchmarkType]benchmarkFactory

// All benchmarks must register themselves at module load time so that deserialization can create a benchmark of that type.
func RegisterBenchmark(btype string, f benchmarkFactory) {
	if benchmarks == nil {
		benchmarks = map[benchmarkType]benchmarkFactory{}
	}
	benchmarks[benchmarkType(btype)] = f
}

type SerializedBenchmark struct {
	Type  benchmarkType
	Input map[string]any
}

type BenchmarkFile []SerializedBenchmark

func DeserializeBenchmark(sb *SerializedBenchmark) (Benchmark, error) {
	found := false
	for b := range benchmarks {
		if sb.Type == b {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown benchmark type: %s", sb.Type)
	}

	return benchmarks[sb.Type](sb.Input)
}
