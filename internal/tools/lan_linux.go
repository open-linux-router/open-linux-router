//go:build linux

package tools

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
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
	cmd.Stdout, cmd.Stderr = nil, nil
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
		return nil, nil, fmt.Errorf("iperf3 could not listen on %s:%d (port busy or address unavailable)", address, port)
	case <-time.After(250 * time.Millisecond):
		return stop, done, nil
	}
}
