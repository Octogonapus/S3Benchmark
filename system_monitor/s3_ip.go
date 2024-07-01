package systemmonitor

import (
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/Octogonapus/S3Benchmark/report"
)

func (mon *systemMonitor) appendS3IPMetrics(now time.Time, buf []byte, s3Prefixes []netip.Prefix) {
	uniqueIPs := 0
	uniqueNetworks := 0
	foundIPs := []string{}
	foundNetworks := []netip.Prefix{}

	for i, line := range strings.Split(string(buf), "\n") {
		parts := strings.Fields(line)
		if i < 2 || len(parts) != 6 {
			continue
		}

		foreignAddressAndPort := parts[4]
		foreignAddress := strings.Split(foreignAddressAndPort, ":")[0]
		parsedForeignAddress, err := netip.ParseAddr(foreignAddress)
		if err != nil {
			slog.Warn("SystemMonitor: failed to parse address", slog.String("address", foreignAddress), slog.String("error", err.Error()))
		}

		for _, prefix := range s3Prefixes {
			if prefix.Contains(parsedForeignAddress) {
				found := false
				for _, foundIP := range foundIPs {
					if foundIP == foreignAddress {
						found = true
						break
					}
				}
				if !found {
					foundIPs = append(foundIPs, foreignAddress)
					uniqueIPs++

					found := false
					for _, foundNetwork := range foundNetworks {
						if foundNetwork == prefix {
							found = true
							break
						}
					}
					if !found {
						foundNetworks = append(foundNetworks, prefix)
						uniqueNetworks++
					}
				}
			}
		}
	}

	mon.sm.S3IPs = append(mon.sm.S3IPs, report.Measurement[int]{
		Time:  now.Unix(),
		Value: uniqueIPs,
	})
	mon.sm.S3Networks = append(mon.sm.S3Networks, report.Measurement[int]{
		Time:  now.Unix(),
		Value: uniqueNetworks,
	})
}
