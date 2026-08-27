//go:build linux

package dhcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

// systemdService drives a unit over the system bus.
//
// D-Bus rather than shelling out to systemctl (design.md §8): the properties
// come back typed, there is no output format to parse and re-parse when systemd
// changes it, and job completion is reported rather than inferred from an exit
// code.
type systemdService struct {
	unit string
}

// NewService returns a Service for a systemd unit.
func NewService(unit string) (Service, error) {
	return systemdService{unit: unit}, nil
}

// connect opens a system bus connection. One per call: these are short
// operations at human timescale, and a cached connection would have to handle
// systemd restarting underneath it.
func (s systemdService) connect(ctx context.Context) (*dbus.Conn, error) {
	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoServiceManager, err)
	}
	return conn, nil
}

func (s systemdService) Status(ctx context.Context) (ServiceStatus, error) {
	conn, err := s.connect(ctx)
	if err != nil {
		return ServiceStatus{Unit: s.unit}, err
	}
	defer conn.Close()

	props, err := conn.GetUnitPropertiesContext(ctx, s.unit)
	if err != nil {
		return ServiceStatus{Unit: s.unit}, fmt.Errorf("reading %s properties: %w", s.unit, err)
	}

	status := ServiceStatus{
		Unit:     s.unit,
		State:    stringProp(props, "ActiveState"),
		SubState: stringProp(props, "SubState"),
		MainPID:  int(uint32Prop(props, "MainPID")),
	}
	status.Active = status.State == "active" || status.State == "reloading"

	// UnitFileState is "enabled", "disabled", "masked", "static", or absent
	// when the unit file is not installed at all.
	switch unitFile := stringProp(props, "UnitFileState"); unitFile {
	case "enabled", "enabled-runtime":
		status.Enabled = true
	}

	if ts := uint64Prop(props, "ActiveEnterTimestamp"); ts > 0 {
		status.Since = time.UnixMicro(int64(ts)).UTC()
	}

	return status, nil
}

func (s systemdService) Start(ctx context.Context) error   { return s.job(ctx, "start") }
func (s systemdService) Stop(ctx context.Context) error    { return s.job(ctx, "stop") }
func (s systemdService) Restart(ctx context.Context) error { return s.job(ctx, "restart") }
func (s systemdService) Reload(ctx context.Context) error  { return s.job(ctx, "reload") }

// job runs a unit job and waits for systemd to report how it finished.
//
// Waiting matters: `olr dhcp set` applies on return (design.md §5.1), so
// returning as soon as the job is *queued* would let the command report success
// while the daemon is still failing to start.
func (s systemdService) job(ctx context.Context, verb string) error {
	conn, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	done := make(chan string, 1)
	switch verb {
	case "start":
		_, err = conn.StartUnitContext(ctx, s.unit, "replace", done)
	case "stop":
		_, err = conn.StopUnitContext(ctx, s.unit, "replace", done)
	case "restart":
		_, err = conn.RestartUnitContext(ctx, s.unit, "replace", done)
	case "reload":
		_, err = conn.ReloadUnitContext(ctx, s.unit, "replace", done)
	default:
		return fmt.Errorf("unknown service verb %q", verb)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", verb, s.unit, err)
	}

	select {
	case result := <-done:
		if result != "done" && result != "skipped" {
			return fmt.Errorf("%s %s: systemd reported %q%s", verb, s.unit, result, s.hint(ctx))
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s %s: %w", verb, s.unit, ctx.Err())
	}
}

// hint appends the daemon's own complaint to a failed job, so the operator does
// not have to go and find it in the journal.
func (s systemdService) hint(ctx context.Context) string {
	status, err := s.Status(ctx)
	if err != nil {
		return ""
	}
	var parts []string
	if status.State != "" {
		parts = append(parts, "state "+status.State)
	}
	if status.SubState != "" {
		parts = append(parts, "sub-state "+status.SubState)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + "); see `journalctl -u " + s.unit + "`"
}

func stringProp(props map[string]any, key string) string {
	if v, ok := props[key].(string); ok {
		return v
	}
	return ""
}

func uint32Prop(props map[string]any, key string) uint32 {
	if v, ok := props[key].(uint32); ok {
		return v
	}
	return 0
}

func uint64Prop(props map[string]any, key string) uint64 {
	if v, ok := props[key].(uint64); ok {
		return v
	}
	return 0
}
