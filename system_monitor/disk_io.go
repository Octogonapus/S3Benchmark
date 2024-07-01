package systemmonitor

import (
	"strconv"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/report"
)

type diskstatEntry struct {
	major                     int
	minor                     int
	deviceName                string
	readsCompleted            int
	readsMerged               int
	sectorsRead               int
	timeSpentReading          int
	writesCompleted           int
	writesMerged              int
	sectorsWritten            int
	timeSpentWriting          int
	iosInProgress             int
	timeSpentDoingIos         int
	weightedTimeSpentDoingIos int
	discardsCompleted         int
	discardsMerged            int
	sectorsDiscarded          int
	timeSpentDiscarding       int
	flushesCompleted          int
	timeSpentFlushing         int
}

func (mon *systemMonitor) appendDiskIOMetrics(now time.Time, buf []byte) {
	for _, line := range strings.Split(string(buf), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 19 {
			continue
		}

		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		readsCompleted, _ := strconv.Atoi(parts[3])
		readsMerged, _ := strconv.Atoi(parts[4])
		sectorsRead, _ := strconv.Atoi(parts[5])
		timeSpentReading, _ := strconv.Atoi(parts[6])
		writesCompleted, _ := strconv.Atoi(parts[7])
		writesMerged, _ := strconv.Atoi(parts[8])
		sectorsWritten, _ := strconv.Atoi(parts[9])
		timeSpentWriting, _ := strconv.Atoi(parts[10])
		iosInProgress, _ := strconv.Atoi(parts[11])
		timeSpentDoingIos, _ := strconv.Atoi(parts[12])
		weightedTimeSpentDoingIos, _ := strconv.Atoi(parts[13])
		discardsCompleted, _ := strconv.Atoi(parts[14])
		discardsMerged, _ := strconv.Atoi(parts[15])
		sectorsDiscarded, _ := strconv.Atoi(parts[16])
		timeSpentDiscarding, _ := strconv.Atoi(parts[17])
		flushesCompleted, _ := strconv.Atoi(parts[18])
		timeSpentFlushing, _ := strconv.Atoi(parts[19])

		entry := diskstatEntry{
			major:                     major,
			minor:                     minor,
			readsCompleted:            readsCompleted,
			readsMerged:               readsMerged,
			sectorsRead:               sectorsRead,
			timeSpentReading:          timeSpentReading,
			writesCompleted:           writesCompleted,
			writesMerged:              writesMerged,
			sectorsWritten:            sectorsWritten,
			timeSpentWriting:          timeSpentWriting,
			iosInProgress:             iosInProgress,
			timeSpentDoingIos:         timeSpentDoingIos,
			weightedTimeSpentDoingIos: weightedTimeSpentDoingIos,
			discardsCompleted:         discardsCompleted,
			discardsMerged:            discardsMerged,
			sectorsDiscarded:          sectorsDiscarded,
			timeSpentDiscarding:       timeSpentDiscarding,
			flushesCompleted:          flushesCompleted,
			timeSpentFlushing:         timeSpentFlushing,
			deviceName:                parts[2],
		}

		readBytes := entry.sectorsRead * 512
		writeBytes := entry.sectorsWritten * 512

		mon.sm.DiskReads = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.readsCompleted,
			},
		})

		mon.sm.DiskReadsMerged = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.readsMerged,
			},
		})

		mon.sm.DiskReadBytes = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: readBytes,
			},
		})

		mon.sm.DiskReadTimeMs = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.timeSpentReading,
			},
		})

		mon.sm.DiskWrites = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.writesCompleted,
			},
		})

		mon.sm.DiskWritesMerged = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.writesMerged,
			},
		})

		mon.sm.DiskWriteBytes = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: writeBytes,
			},
		})

		mon.sm.DiskWriteTimeMs = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.timeSpentWriting,
			},
		})

		mon.sm.DiskIOTimeMs = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.timeSpentDoingIos,
			},
		})

		mon.sm.DiskWeightedIOTimeMs = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.weightedTimeSpentDoingIos,
			},
		})

		mon.sm.DiskFlushes = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.flushesCompleted,
			},
		})

		mon.sm.DiskFlushTimeMs = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.timeSpentFlushing,
			},
		})

		mon.sm.DiskIopsInProgress = append(mon.sm.DiskReads, report.DeviceMeasurement[int]{
			DeviceName: entry.deviceName,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: entry.iosInProgress,
			},
		})
	}
}
