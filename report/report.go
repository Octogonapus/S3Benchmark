package report

type Measurement[T any] struct {
	Time  int64
	Value T
}

type DeviceMeasurement[T any] struct {
	DeviceName  string
	Measurement Measurement[T]
}

type SystemMeasurements struct {
	CpuUsageUser      []Measurement[float64]
	CpuUsageSystem    []Measurement[float64]
	CpuUsageIdle      []Measurement[float64]
	CpuUsageNice      []Measurement[float64]
	CpuUsageIowait    []Measurement[float64]
	CpuUsageIrq       []Measurement[float64]
	CpuUsageSoftIrq   []Measurement[float64]
	CpuUsageSteal     []Measurement[float64]
	CpuUsageGuest     []Measurement[float64]
	CpuUsageGuestNice []Measurement[float64]

	MemTotalBytes  []Measurement[int]
	MemUsedBytes   []Measurement[int]
	MemUsedPct     []Measurement[float64]
	MemAvailBytes  []Measurement[int]
	MemAvailPct    []Measurement[float64]
	SwapTotalBytes []Measurement[int]
	SwapUsedBytes  []Measurement[int]
	SwapUsedPct    []Measurement[float64]

	DiskReads            []DeviceMeasurement[int]
	DiskReadsMerged      []DeviceMeasurement[int]
	DiskReadBytes        []DeviceMeasurement[int]
	DiskReadTimeMs       []DeviceMeasurement[int]
	DiskWrites           []DeviceMeasurement[int]
	DiskWritesMerged     []DeviceMeasurement[int]
	DiskWriteBytes       []DeviceMeasurement[int]
	DiskWriteTimeMs      []DeviceMeasurement[int]
	DiskIOTimeMs         []DeviceMeasurement[int]
	DiskWeightedIOTimeMs []DeviceMeasurement[int]
	DiskFlushes          []DeviceMeasurement[int]
	DiskFlushTimeMs      []DeviceMeasurement[int]
	DiskIopsInProgress   []DeviceMeasurement[int]

	NetBytesSent   []DeviceMeasurement[int]
	NetBytesRecv   []DeviceMeasurement[int]
	NetPacketsSent []DeviceMeasurement[int]
	NetPacketsRecv []DeviceMeasurement[int]

	S3IPs      []Measurement[int]
	S3Networks []Measurement[int]
}

type BenchmarkReport struct {
	Name               string
	Metadata           []any // one entry for each repetition
	Input              map[string]any
	Error              string    // non-empty iff the benchmark failed
	TotalTimeSec       []float64 // one entry for each repetition
	SystemMeasurements *SystemMeasurements
}
