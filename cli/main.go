package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/Octogonapus/S3Benchmark/benchmark"
	benchmarkorchestrator "github.com/Octogonapus/S3Benchmark/benchmark_orchestrator"
	objectprovider "github.com/Octogonapus/S3Benchmark/object_provider"
	"github.com/Octogonapus/S3Benchmark/profile"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type benchmarkFiles []string

func (bfs *benchmarkFiles) String() string {
	return "string rep"
}

func (bfs *benchmarkFiles) Set(value string) error {
	*bfs = append(*bfs, value)
	return nil
}

func main() {
	bucketName := flag.String("bucket-name", "benchmark-bucket-qoizxbnks", "The bucket name.")
	uploadOnly := flag.Bool("upload-only", false, "Only upload objects to a bucket. Creates the bucket if it does not exist. Does not destroy the bucket.")
	skipUpload := flag.Bool("skip-upload", false, "Skip uploading objects. The selected objects must already exist.")
	destroyBucket := flag.Bool("destroy-bucket", true, "Whether to destroy the bucket.")
	uploadConcurrency := flag.Int("upload-concurrency", 36, "The number of goroutines used to upload objects. Additionally, each object automatically uses up to 5 goroutines (multiplicative).")
	objects := flag.String("objects", string(objectprovider.ObjectsSmall), fmt.Sprintf("The built-in object set used for the benchmark. Must be one of: %s.", objectprovider.ExplainObjects()))
	objectsPath := flag.String("objects-path", "", "A path to a CSV file containing object keys and sizes. Overrides the selected built-in object set.")
	profiler := flag.String("profiler", "none", fmt.Sprintf("The type of profiler to use. No profiler is used by default. When profiling, benchmark results are discarded. Must be one of: %s.", profile.ExplainProfilers()))
	profileSaveDir := flag.String("profile-dir", ".", "Save profiling results into this directory.")
	bfiles := benchmarkFiles{}
	flag.Var(&bfiles, "benchmark-file", "The benchmark configuration file containing all the benchmark specifications. Can be used multiple times; all benchmarks will be loaded. At least one is required.")
	benchmarkConcurrency := flag.Int("benchmark-concurrency", 0, "How many benchmarks can be run concurrently. Unlimited by default.")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	if len(bfiles) == 0 {
		panic(fmt.Errorf("benchmark-file is a required flag"))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithEC2IMDSRegion())
	if err != nil {
		panic(err)
	}

	var objectSpecs []*objectprovider.ObjectSpec
	if *objectsPath != "" {
		buf, err := os.ReadFile(*objectsPath)
		if err != nil {
			panic(err)
		}
		objectSpecs, err = objectprovider.LoadObjectSpecsFromBuf(buf)
		if err != nil {
			panic(err)
		}
	} else {
		objectSpecs, err = objectprovider.LoadBuiltinObjectSpecs(objectprovider.Objects(*objects))
		if err != nil {
			panic(err)
		}
	}

	objProvider := objectprovider.NewS3ObjectProvider(&objectprovider.S3ObjectProviderInput{
		AwsConfig:         cfg,
		Bucket:            *bucketName,
		UploadConcurrency: *uploadConcurrency,
	})
	objProvider.SetObjects(objectSpecs)

	if *uploadOnly {
		err = objProvider.SetUp()
		if err != nil {
			panic(err)
		}
		// Don't tear down on purpose

		err = objProvider.MakeObjects()
		if err != nil {
			panic(err)
		}
		return
	} else if !*skipUpload {
		err = objProvider.SetUp()
		if err != nil {
			panic(err)
		}
		if *destroyBucket {
			defer objProvider.TearDown()
		}

		err = objProvider.MakeObjects()
		if err != nil {
			panic(err)
		}
	}

	orch, err := benchmarkorchestrator.NewEC2BenchmarkOrchestrator(&benchmarkorchestrator.EC2BenchmarkOrchestratorInput{
		AwsConfig: cfg,
		InstanceTypes: []ec2Types.InstanceType{
			ec2Types.InstanceTypeM6i8xlarge,
		},
		WaitToInitialize:     false, // TODO make flag and set true by default
		Bucket:               objProvider.GetBucket(),
		ProfilerKind:         profile.ProfilerKind(*profiler),
		ProfileSaveDir:       *profileSaveDir,
		BenchmarkConcurrency: *benchmarkConcurrency,
	})
	if err != nil {
		panic(err)
	}

	for _, bf := range bfiles {
		bfData, err := os.ReadFile(bf)
		if err != nil {
			panic(err)
		}
		benchmarks := benchmark.BenchmarkFile{}
		err = json.Unmarshal(bfData, &benchmarks)
		if err != nil {
			panic(err)
		}
		for _, sb := range benchmarks {
			b, err := benchmark.DeserializeBenchmark(&sb)
			if err != nil {
				panic(err)
			}
			err = orch.AddBenchmark(b)
			if err != nil {
				panic(err)
			}
		}
	}

	var objectsDesc string
	var objectsName string
	if *objectsPath != "" {
		objectsName = "custom (from path)"
		objectsDesc = ""
	} else {
		objectsName = *objects
		objectsDesc = objectprovider.AllObjectsWithDescriptions[objectprovider.Objects(*objects)]
	}

	resultDir := "results"
	err = orch.SetUp(&benchmarkorchestrator.BenchmarkConfig{
		ObjectsName:   objectsName,
		ObjectsDesc:   objectsDesc,
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
