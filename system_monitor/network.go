package systemmonitor

import (
	"strconv"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/report"
)

func (mon *systemMonitor) appendNetworkMetrics(now time.Time, buf []byte) {
	for _, line := range strings.Split(string(buf), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 17 {
			continue
		}

		iface := parts[0][:len(parts[0])-1]
		recvBytes, _ := strconv.Atoi(parts[1])
		recvPackets, _ := strconv.Atoi(parts[2])
		sendBytes, _ := strconv.Atoi(parts[9])
		sendPackets, _ := strconv.Atoi(parts[10])

		mon.sm.NetBytesSent = append(mon.sm.NetBytesSent, report.DeviceMeasurement[int]{
			DeviceName: iface,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: sendBytes,
			},
		})

		mon.sm.NetBytesRecv = append(mon.sm.NetBytesRecv, report.DeviceMeasurement[int]{
			DeviceName: iface,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: recvBytes,
			},
		})
		mon.sm.NetPacketsSent = append(mon.sm.NetPacketsSent, report.DeviceMeasurement[int]{
			DeviceName: iface,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: sendPackets,
			},
		})
		mon.sm.NetPacketsRecv = append(mon.sm.NetPacketsRecv, report.DeviceMeasurement[int]{
			DeviceName: iface,
			Measurement: report.Measurement[int]{
				Time:  now.Unix(),
				Value: recvPackets,
			},
		})
	}
}
