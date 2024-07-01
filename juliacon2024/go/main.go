package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/Octogonapus/S3Benchmark/benchmark/gobench"
	benchmarkorchestrator "github.com/Octogonapus/S3Benchmark/benchmark_orchestrator"
	objectprovider "github.com/Octogonapus/S3Benchmark/object_provider"
	"github.com/Octogonapus/S3Benchmark/profile"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithEC2IMDSRegion())
	if err != nil {
		panic(err)
	}

	// objects := objectprovider.Objects87GiB50k
	objects := objectprovider.Objects100GiB10
	objectSpecs, err := objectprovider.LoadBuiltinObjectSpecs(objects)
	if err != nil {
		panic(err)
	}

	objProvider := objectprovider.NewS3ObjectProvider(&objectprovider.S3ObjectProviderInput{
		AwsConfig:         cfg,
		Bucket:            "benchmark-bucket-qoizxbnks",
		UploadConcurrency: 36,
	})
	objProvider.SetObjects(objectSpecs)

	err = objProvider.SetUp()
	if err != nil {
		panic(err)
	}
	// don't destroy bucket

	// err = objProvider.MakeObjects()
	// if err != nil {
	// 	panic(err)
	// }

	orch, err := benchmarkorchestrator.NewEC2BenchmarkOrchestrator(&benchmarkorchestrator.EC2BenchmarkOrchestratorInput{
		AwsConfig: cfg,
		InstanceTypes: []ec2Types.InstanceType{
			ec2Types.InstanceTypeM6i8xlarge,
		},
		WaitToInitialize:     false,
		Bucket:               objProvider.GetBucket(),
		ProfilerKind:         profile.None,
		BenchmarkConcurrency: 5,
		BenchmarkRuns:        3,
	})
	if err != nil {
		panic(err)
	}

	for _, concurrency := range []int{32, 64, 128, 256} {
		b, err := gobench.NewGoBenchmark(&gobench.GoBenchmarkInput{
			Name:                fmt.Sprintf("go, %d goroutines, no parts", concurrency),
			DownloadConcurrency: concurrency,
			DownloadInParts:     false,
		})
		if err != nil {
			panic(err)
		}
		orch.AddBenchmark(b)
	}

	for _, concurrency := range []int{32, 64, 128, 256} {
		for _, partSize := range []int{1024 * 1024 * 5, 1024 * 1024 * 10} {
			for _, partConcurrency := range []int{5, 10} {
				b, err := gobench.NewGoBenchmark(&gobench.GoBenchmarkInput{
					Name:                fmt.Sprintf("go, %d goroutines, %d per-part goroutines, partsize=%dMiB", concurrency, partConcurrency, partSize/(1024*1024)),
					DownloadConcurrency: concurrency,
					DownloadInParts:     true,
					PartSize:            partSize,
					PartConcurrency:     partConcurrency,
				})
				if err != nil {
					panic(err)
				}
				orch.AddBenchmark(b)
			}
		}
	}

	resultDir := "results"
	err = orch.SetUp(&benchmarkorchestrator.BenchmarkConfig{
		ObjectsName:   string(objects),
		ObjectsDesc:   objectprovider.AllObjectsWithDescriptions[objects],
		ObjectSpecs:   objProvider.GetObjects(),
		ResultDir:     resultDir,
		WarmUpObjects: false,
	})
	defer orch.TearDown()
	if err != nil {
		panic(err)
	}

	report, err := orch.RunBenchmarks()
	if err != nil {
		panic(err)
	}

	bytes, err := json.Marshal(report)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(path.Join(resultDir, "report.json"), bytes, os.ModePerm)
	if err != nil {
		panic(err)
	}
}
