package objectprovider

import (
	_ "embed"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

//go:embed objects_87gb_50k.csv
var objects87GB50kCSV []byte

//go:embed objects_prefix_contention.csv
var objectsPrefixContentionCSV []byte

//go:embed objects_100gb_10.csv
var objects100GB10CSV []byte

type Objects string

const (
	ObjectsSmall            Objects = "small"
	ObjectsMedium           Objects = "medium"
	Objects87GiB50k         Objects = "87GiB50k"
	ObjectsPrefixContention Objects = "PrefixContention"
	Objects100GiB10         Objects = "100GiB10"
)

var AllObjectsWithDescriptions = map[Objects]string{
	ObjectsSmall:            "3 small objects for testing",
	ObjectsMedium:           "100 small objects for testing",
	Objects87GiB50k:         "50,000 objects totaling 87 GiB uniformly distributed",
	ObjectsPrefixContention: "60,000 1 KiB objects under one prefix",
	Objects100GiB10:         "10 10 GiB objects uniformly distributed",
}

type ObjectSpec struct {
	Key       string
	SizeBytes int
}

type ObjectProvider interface {
	// Create the objects using the current object specs.
	MakeObjects() error

	// Create any resources needed before MakeObjects can be ran.
	SetUp() error

	// Destroy any resources created by SetUp.
	TearDown() error

	// Set the object specs to be created by MakeObjects. Do not create any objects.
	SetObjects([]*ObjectSpec)

	GetObjects() []*ObjectSpec

	GetBucket() string
}

func LoadBuiltinObjectSpecs(objects Objects) ([]*ObjectSpec, error) {
	switch objects {
	case ObjectsSmall:
		objs, err := LoadObjectSpecsFromBuf(objects87GB50kCSV)
		if err != nil {
			return nil, err
		}
		return objs[0:3], nil
	case ObjectsMedium:
		objs, err := LoadObjectSpecsFromBuf(objects87GB50kCSV)
		if err != nil {
			return nil, err
		}
		return objs[0:100], nil
	case Objects87GiB50k:
		return LoadObjectSpecsFromBuf(objects87GB50kCSV)
	case ObjectsPrefixContention:
		return LoadObjectSpecsFromBuf(objectsPrefixContentionCSV)
	case Objects100GiB10:
		return LoadObjectSpecsFromBuf(objects100GB10CSV)
	default:
		return nil, fmt.Errorf("unknown objects builtin: %s", string(objects))
	}
}

func LoadObjectSpecsFromBuf(buf []byte) ([]*ObjectSpec, error) {
	lines := strings.Split(string(buf), "\n")
	out := []*ObjectSpec{}
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}

		size, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(out, func(it *ObjectSpec) bool {
			return it.Key == parts[0]
		}) {
			return nil, fmt.Errorf("duplicate key: %s", parts[0])
		}

		os := &ObjectSpec{
			Key:       parts[0],
			SizeBytes: size,
		}
		out = append(out, os)
	}
	return out, nil
}

func ExplainObjects() string {
	i := 0
	var sb strings.Builder
	for obj, desc := range AllObjectsWithDescriptions {
		sb.WriteString("\"")
		sb.WriteString(string(obj))
		sb.WriteString("\"")
		sb.WriteString(" (")
		sb.WriteString(desc)
		sb.WriteString(")")
		if i < len(AllObjectsWithDescriptions)-1 {
			sb.WriteString(", ")
		}
		i++
	}
	return sb.String()
}
