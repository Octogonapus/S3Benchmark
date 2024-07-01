package benchmarkorchestrator

import (
	"github.com/Octogonapus/S3Benchmark/benchmark"
	objectprovider "github.com/Octogonapus/S3Benchmark/object_provider"
	"github.com/Octogonapus/S3Benchmark/report"
)

type BenchmarkConfig struct {
	ObjectsName   string
	ObjectsDesc   string
	ObjectSpecs   []*objectprovider.ObjectSpec
	ResultDir     string
	WarmUpObjects bool
}

type Report struct {
	Config  *BenchmarkConfig
	Reports []*report.BenchmarkReport
}

// Runs benchmarks on a platform (e.g. AWS EC2).
type BenchmarkOrchestrator interface {
	// Add a benchmark to be ran later.
	AddBenchmark(benchmark.Benchmark) error

	// Set up the environment.
	SetUp(*BenchmarkConfig) error

	// Run benchmarks (concurrently) and return a report.
	RunBenchmarks() (*Report, error)

	// Tear down the environment.
	TearDown() error
}
