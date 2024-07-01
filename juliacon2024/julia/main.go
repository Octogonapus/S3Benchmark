package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/Octogonapus/S3Benchmark/benchmark/julia_awsjl"
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

	// objects := objectprovider.ObjectsMedium
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
		ProfileSaveDir:       "results",
		BenchmarkConcurrency: 10,
		BenchmarkRuns:        3,
	})
	if err != nil {
		panic(err)
	}

	// juliaawsjl.GreedyThreads is worse
	// juliaawsjl.KeepOffThread1 is a lot worse
	for _, nthreads := range []int{16, 32, 48, 64} {
		for _, backend := range []julia_awsjl.JuliaAwsjlBackend{julia_awsjl.HTTP, julia_awsjl.Downloads} {
			for _, useParts := range []bool{true, false} {
				b, err := julia_awsjl.NewJuliaAwsjlBenchmark(&julia_awsjl.JuliaAwsjlBenchmarkInput{
					Name:                  fmt.Sprintf("Julia AWS.jl (backend=%s), %d threads, parts=%t", backend, nthreads, useParts),
					JuliaVersion:          "1.10.4",
					Nthreads:              nthreads,
					AwsBackend:            backend,
					WriteToDisk:           false,
					DownloadStrategy:      julia_awsjl.DynamicThreads,
					DownloadInParts:       useParts,
					DownloadPartSizeBytes: 1024 * 1024 * 10,
					DownloadPartsNThreads: 5,
				})
				if err != nil {
					panic(err)
				}
				orch.AddBenchmark(b)
			}
		}

		// b, err := julia_http2.NewJuliaHttp2Benchmark(&julia_http2.JuliaHttp2BenchmarkInput{
		// 	Name:         fmt.Sprintf("Julia CloudStore.jl + HTTP2.jl, %d threads", nthreads),
		// 	JuliaVersion: "1.10.4",
		// 	Nthreads:     nthreads,
		// })
		// if err != nil {
		// 	panic(err)
		// }
		// orch.AddBenchmark(b)
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
