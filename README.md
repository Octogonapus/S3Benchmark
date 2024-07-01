# S3Benchmark

A collection of benchmarks for evaluating the performance of transferring S3 objects. Primarily for S3 -> EC2 downloads.

## Background

The [S3 User Guide](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance.html) states:

1. Make concurrent requests of 8-16 MB in size
2. One request per 85-90 MB/s of desired throughput
3. Monitor CPU usage and other system metrics to ensure they are not the bottleneck
4. Monitor DNS requests to ensure connections are spread over a wide pool of S3 IPs (to use load balancing)
5. A prefix is limited to 3,500 PUT/COPY/POST/DELETE or 5,500 GET/HEAD
   1. The number of prefixes is unlimited
   2. S3 dynamically scales prefix capacity and you may receive 503 errors during scaling
   3. [Prefixes are split lexicographically; consider if S3 will need to scale when you add a new prefix](https://youtu.be/sYDJYqvNeXU?t=1257)
6. A single EC2 instance can achieve up to 100 Gb/s to S3 using a VPC Endpoint and within one region
7. Small object/first-byte-out latency is 100-200 milliseconds

Unknowns to characterize:

1. Are all objects equal? Based on how S3 distributes an object, one object may have a favorable distribution.
2. Does time matter? Does S3 move an object around after the initial put? Does get performance change x minutes after the initial put?
3. Prefix scaling exists but it hasn't been measured as far as I can tell.

Things tested by this project:

1. Number of objects
2. Object size
3. Request size
4. Concurrent request count
5. Backends
   1. AWS CLI
   2. Go
   3. Julia (HTTP.jl, HTTP2.jl, Downloads.jl)

## Usage

This project contains a set of configurable benchmarks and ways to run those benchmarks. Some configuration is required.
You need to decide on a set of benchmarks to run, objects (data in S3) to run them on, and how to run them.
There is a [CLI](./cli/main.go) which allows you to pass in a JSON file containing a list of benchmark specifications.
Users requiring more customization should write a Go program instead; examples are in [juliacon2024](./juliacon2024/).

## Architecture

This project consists of these main components:

1. [Benchmark](./benchmark/benchmark.go)
2. [Target](./target/target.go)
3. [Benchmark Orchestrator](./benchmark_orchestrator/benchmark_orchestrator.go)
4. [Profiler](./profile/profiler.go)

These components (especially benchmarks) are as encapsulated as possible to make extension simple.
At a high level, benchmark orchestrators implement the platform (e.g. AWS), create targets (e.g. an EC2 instance),
and run benchmarks on those targets.

If you want to add a new benchmark, an example to look at is [go_benchmark.go](./benchmark/gobench/go_benchmark.go).

If you want to add a new platform (e.g. Azure instead of AWS), you will need to add a new benchmark orchestrator. This is a lot more involed; take a look at [ec2_benchmark_orchestrator.go](./benchmark_orchestrator/ec2_benchmark_orchestrator.go).
