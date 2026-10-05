package system

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Metrics is a snapshot of the Linux host, not the daemon process.
type Metrics struct {
	UptimeSeconds uint64   `json:"uptime_seconds"`
	CPUUsedCores  *float64 `json:"cpu_used_cores"`
	CPUCores      int      `json:"cpu_cores"`
	MemoryUsed    uint64   `json:"memory_used_bytes"`
	MemoryTotal   uint64   `json:"memory_total_bytes"`
}

func readMetrics(proc string) (Metrics, error) {
	var out Metrics
	data, err := os.ReadFile(proc + "/uptime")
	if err != nil {
		return out, err
	}
	seconds, err := strconv.ParseFloat(strings.Fields(string(data))[0], 64)
	if err != nil {
		return out, fmt.Errorf("uptime: %w", err)
	}
	out.UptimeSeconds = uint64(seconds)
	data, err = os.ReadFile(proc + "/meminfo")
	if err != nil {
		return out, err
	}
	var total, available uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = value * 1024
		case "MemAvailable:":
			available = value * 1024
		}
	}
	if total == 0 || available > total {
		return out, fmt.Errorf("invalid memory counters")
	}
	out.MemoryTotal, out.MemoryUsed = total, total-available
	file, err := os.Open(proc + "/stat")
	if err != nil {
		return out, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "cpu") && len(scanner.Text()) > 3 && scanner.Text()[3] >= '0' && scanner.Text()[3] <= '9' {
			out.CPUCores++
		}
	}
	return out, scanner.Err()
}

type cpuSample struct{ total, idle uint64 }

func readCPU(path string) (cpuSample, error) {
	var sample cpuSample
	data, err := os.ReadFile(path)
	if err != nil {
		return sample, err
	}
	fields := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return sample, fmt.Errorf("invalid CPU counters")
	}
	for i, field := range fields[1:] {
		value, parseErr := strconv.ParseUint(field, 10, 64)
		if parseErr != nil {
			return sample, parseErr
		}
		sample.total += value
		if i == 3 || i == 4 {
			sample.idle += value
		}
	}
	return sample, nil
}
