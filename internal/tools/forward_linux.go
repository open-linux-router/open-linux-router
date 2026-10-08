//go:build linux

package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type ForwardSample struct {
	Second int     `json:"second"`
	Mbps   float64 `json:"mbps"`
}

type ForwardRun struct {
	Name         string          `json:"name"`
	PathVerified bool            `json:"path_verified"`
	Mbps         float64         `json:"mbps"`
	PPS          float64         `json:"pps,omitempty"`
	Loss         float64         `json:"loss_percent,omitempty"`
	Samples      []ForwardSample `json:"samples"`
}

type ForwardResult struct {
	Runs           []ForwardRun `json:"runs"`
	Note           string       `json:"note"`
	NATStatus      string       `json:"nat_status"`
	FirewallStatus string       `json:"firewall_status"`
}

func command(ctx context.Context, binary string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", binary, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func forwardBenchmark(ctx context.Context) (ForwardResult, error) {
	ip, iperf := core.LookTool("ip"), core.LookTool("iperf3")
	if ip == "" || iperf == "" {
		return ForwardResult{}, errors.New("install iproute2 and iperf3 to run the local forwarding test")
	}
	enabled, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil || strings.TrimSpace(string(enabled)) != "1" {
		return ForwardResult{}, errors.New("IPv4 forwarding is disabled; enable the gateway before testing")
	}
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ForwardResult{}, err
	}
	suffix := hex.EncodeToString(nonce[:])
	// Do not replace a host route already using the reserved benchmark subnet.
	out, err := command(ctx, ip, "-j", "route", "show", "198.19.254.0/29")
	if err != nil {
		return ForwardResult{}, fmt.Errorf("checking benchmark subnet: %w", err)
	}
	if strings.TrimSpace(string(out)) != "[]" {
		return ForwardResult{}, errors.New("benchmark subnet 198.19.254.0/29 is already in use")
	}

	a, b := "olr-a-"+suffix, "olr-b-"+suffix
	va, vb := "oa"+suffix, "ob"+suffix
	// Names and addresses are confined to temporary namespaces and veth links.
	cleanup := func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = command(stop, ip, "netns", "del", a)
		_, _ = command(stop, ip, "netns", "del", b)
		_, _ = command(stop, ip, "link", "del", va)
		_, _ = command(stop, ip, "link", "del", vb)
	}
	defer cleanup()
	setup := [][]string{
		{"netns", "add", a}, {"netns", "add", b},
		{"link", "add", va, "type", "veth", "peer", "name", "pa" + suffix},
		{"link", "add", vb, "type", "veth", "peer", "name", "pb" + suffix},
		{"link", "set", "pa" + suffix, "netns", a}, {"link", "set", "pb" + suffix, "netns", b},
		{"addr", "add", "198.19.254.1/30", "dev", va}, {"addr", "add", "198.19.254.5/30", "dev", vb},
		{"link", "set", va, "up"}, {"link", "set", vb, "up"},
		{"-n", a, "addr", "add", "198.19.254.2/30", "dev", "pa" + suffix},
		{"-n", b, "addr", "add", "198.19.254.6/30", "dev", "pb" + suffix},
		{"-n", a, "link", "set", "pa" + suffix, "up"}, {"-n", b, "link", "set", "pb" + suffix, "up"},
		{"-n", a, "route", "add", "198.19.254.4/30", "via", "198.19.254.1"},
		{"-n", b, "route", "add", "198.19.254.0/30", "via", "198.19.254.5"},
	}
	for _, args := range setup {
		if _, err := command(ctx, ip, args...); err != nil {
			return ForwardResult{}, fmt.Errorf("setting up isolated test network: %w", err)
		}
	}
	result := ForwardResult{
		Runs:           make([]ForwardRun, 0, 3),
		NATStatus:      "Not tested: temporary sources and virtual egress do not match the router's configured LAN and uplink.",
		FirewallStatus: "Not tested: temporary interfaces are not the router's configured inside interfaces.",
		Note:           "Virtual software forwarding only. Generator and receiver share CPU with the router; physical NICs are bypassed. This is not a guaranteed WAN speed.",
	}
	for _, stage := range []struct {
		name string
		args []string
		udp  bool
	}{
		{"Virtual A to B (TCP)", []string{"-P", "4"}, false},
		{"Virtual B to A (TCP)", []string{"-P", "4", "-R"}, false},
		{"Small packets (UDP, 128 B)", []string{"-u", "-l", "128", "-b", "100M"}, true},
	} {
		run, err := forwardStage(ctx, ip, iperf, a, b, va, vb, stage.name, stage.args, stage.udp)
		if err != nil {
			return ForwardResult{}, err
		}
		result.Runs = append(result.Runs, run)
	}
	return result, nil
}

func forwardStage(ctx context.Context, ip, iperf, a, b, va, vb, name string, options []string, udp bool) (ForwardRun, error) {
	ingress, egress := va, vb
	if slices.Contains(options, "-R") {
		ingress, egress = vb, va
	}
	beforeIn, err := interfaceBytes(ingress, "rx_bytes")
	if err != nil {
		return ForwardRun{}, err
	}
	beforeOut, err := interfaceBytes(egress, "tx_bytes")
	if err != nil {
		return ForwardRun{}, err
	}
	server := exec.CommandContext(ctx, ip, "netns", "exec", b, iperf, "-s", "-1", "-B", "198.19.254.6")
	if err := server.Start(); err != nil {
		return ForwardRun{}, fmt.Errorf("starting test receiver: %w", err)
	}
	defer func() {
		if server.Process != nil {
			_ = server.Process.Kill()
			_, _ = server.Process.Wait()
		}
	}()
	select {
	case <-ctx.Done():
		return ForwardRun{}, ctx.Err()
	case <-time.After(350 * time.Millisecond):
	}
	args := []string{"netns", "exec", a, iperf, "-c", "198.19.254.6", "-t", "5", "-i", "1", "-J"}
	args = append(args, options...)
	output, err := command(ctx, ip, args...)
	if err != nil {
		return ForwardRun{}, fmt.Errorf("%s: %w (the host firewall may block the temporary path)", name, err)
	}
	run, err := parseForwardRun(name, output, udp)
	if err != nil {
		return ForwardRun{}, err
	}
	afterIn, err := interfaceBytes(ingress, "rx_bytes")
	if err != nil {
		return ForwardRun{}, err
	}
	afterOut, err := interfaceBytes(egress, "tx_bytes")
	if err != nil {
		return ForwardRun{}, err
	}
	// Both host-side links must carry substantial traffic in the expected direction.
	if afterIn <= beforeIn+100_000 || afterOut <= beforeOut+100_000 {
		return ForwardRun{}, fmt.Errorf("%s: forwarding path could not be verified on both virtual links", name)
	}
	run.PathVerified = true
	return run, nil
}

func interfaceBytes(name, counter string) (uint64, error) {
	// Names are generated from a random hex suffix; never use a caller-supplied path.
	raw, err := os.ReadFile(filepath.Join("/sys/class/net", name, "statistics", counter))
	if err != nil {
		return 0, fmt.Errorf("reading %s %s: %w", name, counter, err)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %s %s: %w", name, counter, err)
	}
	return value, nil
}

func parseForwardRun(name string, output []byte, udp bool) (ForwardRun, error) {
	var data struct {
		Error     string `json:"error"`
		Intervals []struct {
			Sum struct {
				Bits float64 `json:"bits_per_second"`
			} `json:"sum"`
		} `json:"intervals"`
		End struct {
			Sent struct {
				Bits float64 `json:"bits_per_second"`
			} `json:"sum_sent"`
			Received struct {
				Bits    float64 `json:"bits_per_second"`
				Packets int64   `json:"packets"`
				Lost    int64   `json:"lost_packets"`
				Seconds float64 `json:"seconds"`
			} `json:"sum_received"`
		} `json:"end"`
	}
	if err := json.Unmarshal(output, &data); err != nil {
		return ForwardRun{}, fmt.Errorf("reading iperf3 result: %w", err)
	}
	if data.Error != "" {
		return ForwardRun{}, errors.New(data.Error)
	}
	bits := data.End.Received.Bits
	if bits <= 0 {
		return ForwardRun{}, errors.New("no forwarded traffic received")
	}
	run := ForwardRun{Name: name, Mbps: bits / 1e6, Samples: make([]ForwardSample, 0, len(data.Intervals))}
	for i, interval := range data.Intervals {
		run.Samples = append(run.Samples, ForwardSample{Second: i + 1, Mbps: interval.Sum.Bits / 1e6})
	}
	if udp {
		total := data.End.Received.Packets
		if data.End.Received.Seconds > 0 {
			run.PPS = float64(total-data.End.Received.Lost) / data.End.Received.Seconds
		}
		if total > 0 {
			run.Loss = 100 * float64(data.End.Received.Lost) / float64(total)
		}
	}
	return run, nil
}
