//go:build linux

package tools

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func startIperf(address string, port int) (func(), <-chan struct{}, error) {
	binary := core.LookTool("iperf3")
	if binary == "" {
		return nil, nil, errors.New("iperf3 is not installed on the router")
	}
	cmd := exec.Command(binary, "-s", "-B", address, "-p", strconv.Itoa(port))
	var startupError limitedBuffer
	cmd.Stderr = &startupError
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("starting iperf3: %w", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	stop := func() {
		select {
		case <-done:
			return
		default:
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}
	select {
	case <-done:
		return nil, nil, fmt.Errorf("iperf3 could not listen on %s:%d: %s", address, port, strings.TrimSpace(startupError.String()))
	case <-time.After(250 * time.Millisecond):
		return stop, done, nil
	}
}

// probeIperf recognises a pre-existing server without adopting or stopping it.
func probeIperf(address string) (string, error) {
	used, err := core.TCPPortInUse(lanPort)
	if err != nil {
		return "", fmt.Errorf("checking TCP port %d: %w", lanPort, err)
	}
	if !used {
		return "", nil
	}
	holder, found := core.TCPPortHolder(lanPort)
	label := "another process"
	if found {
		label = holder.String()
	}
	if !found || holder.Name != "iperf3" {
		return "", fmt.Errorf("TCP port %d is held by %s; OLR will not stop it", lanPort, label)
	}
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort(address, strconv.Itoa(lanPort)), 300*time.Millisecond)
	if err != nil {
		return "", fmt.Errorf("%s holds TCP port %d but is not reachable at %s: %v", label, lanPort, address, err)
	}
	_ = conn.Close()
	return label, nil
}
