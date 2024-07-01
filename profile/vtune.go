package profile

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/Octogonapus/S3Benchmark/target"
	"github.com/Octogonapus/S3Benchmark/util"
)

type vtune struct {
	target target.Target
}

func init() {
	RegisterProfiler(VTune, NewVTune)
}

func NewVTune(target target.Target) Profiler {
	return &vtune{target: target}
}

func (v *vtune) SetUp() error {
	out, err := v.target.RunCommand("apt update -y && apt install -y gpg-agent wget")
	if err != nil {
		slog.Error("VTune: installing deps failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return fmt.Errorf("installing deps failed: %w", err)
	}
	out, err = v.target.RunCommand("wget -O- https://apt.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB | gpg --dearmor | tee /usr/share/keyrings/oneapi-archive-keyring.gpg > /dev/null")
	if err != nil {
		slog.Error("VTune: installing intel gpg key failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return fmt.Errorf("installing intel gpg key failed: %w", err)
	}
	out, err = v.target.RunCommand("echo 'deb [signed-by=/usr/share/keyrings/oneapi-archive-keyring.gpg] https://apt.repos.intel.com/oneapi all main' | tee /etc/apt/sources.list.d/oneAPI.list")
	if err != nil {
		slog.Error("VTune: adding intel repo failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return fmt.Errorf("adding intel repo failed: %w", err)
	}
	out, err = v.target.RunCommand("apt update -y && apt install -y intel-oneapi-vtune")
	if err != nil {
		slog.Error("VTune: installing vtune failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return fmt.Errorf("installing vtune failed: %w", err)
	}

	// Not sure if the sampling drivers are needed but this would do it
	// out, err = v.target.RunCommand("cd /opt/intel/oneapi/vtune/latest/sepdk/src && ./build-driver -ni && ./insmod-sep -r -g root")
	// if err != nil {
	// 	slog.Error("VTune: installing sampling drivers failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
	// 	return fmt.Errorf("installing sampling drivers failed: %w", err)
	// }

	// Don't run the self check because lots of things can fail and all we need is software-based hotspot

	return nil
}

func (v *vtune) ProfileCommand(cmd string) (string, error) {
	cmd = strings.ReplaceAll(cmd, "\\", "\\\\")
	cmd = strings.ReplaceAll(cmd, "\"", "\\\"")

	resultDir := fmt.Sprintf("r%s", util.Randstring(8))
	out, err := v.target.RunCommand(fmt.Sprintf("source /opt/intel/oneapi/vtune/latest/env/vars.sh && vtune -collect hotspots -knob sampling-mode=sw -knob enable-stack-collection=true -result-dir=%s -- %s", resultDir, cmd))
	if err != nil {
		slog.Error("VTune: reporting failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return "", fmt.Errorf("collection failed: %w", err)
	}
	resultFile := fmt.Sprintf("/root/%s.tar.xz", resultDir)
	out, err = v.target.RunCommand(fmt.Sprintf("tar -czf %s %s", resultFile, resultDir))
	if err != nil {
		slog.Error("VTune: compressing result dir failed", slog.String("command output", string(out)), slog.String("error", err.Error()))
		return "", fmt.Errorf("compressing result dir failed: %w", err)
	}
	return resultFile, nil
}
