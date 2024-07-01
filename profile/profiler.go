package profile

import (
	"fmt"
	"strings"

	"github.com/Octogonapus/S3Benchmark/target"
)

type Profiler interface {
	SetUp() error
	ProfileCommand(cmd string) (string, error)
}

type ProfilerKind string

const (
	None  ProfilerKind = "none"
	VTune ProfilerKind = "vtune"
)

type ProfilerFactory func(target.Target) Profiler

var allProfilers map[ProfilerKind]ProfilerFactory

func RegisterProfiler(kind ProfilerKind, factory ProfilerFactory) {
	if allProfilers == nil {
		allProfilers = map[ProfilerKind]ProfilerFactory{
			None: func(t target.Target) Profiler { panic("Profiler kind none is reserved and can't be created") },
		}
	}
	allProfilers[kind] = factory
}

func NewProfiler(kind ProfilerKind, target target.Target) (Profiler, error) {
	if kind == None {
		return nil, fmt.Errorf("Profiler kind none is reserved and can't be created")
	}

	factory, ok := allProfilers[kind]
	if !ok {
		return nil, fmt.Errorf("unknown profiler kind: %s", kind)
	}
	return factory(target), nil
}

func ExplainProfilers() string {
	i := 0
	var sb strings.Builder
	for kind := range allProfilers {
		sb.WriteString("\"")
		sb.WriteString(string(kind))
		sb.WriteString("\"")
		if i < len(allProfilers)-1 {
			sb.WriteString(", ")
		}
		i++
	}
	return sb.String()
}
