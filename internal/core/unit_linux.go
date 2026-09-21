//go:build linux

package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

// A bus connection scoped to one request.
//
// The per-call connection below is right for a mutation: those happen once, at
// human timescale, when somebody presses a button. It is wrong for a status
// read. One GET /api/dns/status asks systemd about five units — the module's
// two, plus the three distribution units that would take :53 — and opened five
// connections to do it, each with its own connect and authentication handshake.
// The overview page polls five modules like that every five seconds.
//
// So reads may share one, for as long as a request lasts and no longer. That
// bound is the point: the objection to a cached connection is that it has to
// cope with systemd restarting underneath it, and a connection that cannot
// outlive a single reply never has to.
//
// It rides on the context because the alternative is threading a connection
// through the Unit interface, which every module implements and most of them
// only ever use to ask one question. Absent — a CLI call, a test, any code that
// never asked for sharing — every read connects for itself exactly as before.

type sharedConnKey struct{}

type sharedConn struct {
	mu       sync.Mutex
	conn     *dbus.Conn
	err      error
	released bool
}

// WithSharedUnitConn scopes one bus connection to ctx, for unit reads made
// while it is alive. It returns the derived context and a release that must be
// called; nothing is dialled unless a read actually asks for it.
func WithSharedUnitConn(ctx context.Context) (context.Context, func()) {
	s := &sharedConn{}
	return context.WithValue(ctx, sharedConnKey{}, s), s.release
}

func (s *sharedConn) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = true
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

// get returns the shared connection, dialling it on first use. A nil connection
// and a nil error mean the scope is over and the caller should dial its own.
//
// A failed dial is remembered for the rest of the scope rather than retried per
// unit. The bus does not come back within one reply, and five identical
// "no service manager" errors cost five timeouts to say what one says.
func (s *sharedConn) get(ctx context.Context) (*dbus.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.released:
		return nil, nil
	case s.err != nil:
		return nil, s.err
	case s.conn != nil:
		return s.conn, nil
	}

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		s.err = fmt.Errorf("%w: %v", ErrNoServiceManager, err)
		return nil, s.err
	}
	s.conn = conn
	return conn, nil
}

// readConn returns a connection for a read-only call, preferring the one shared
// across the current request. The returned release must be called, and closes
// the connection only when it is not the shared one.
func readConn(ctx context.Context) (*dbus.Conn, func(), error) {
	if s, ok := ctx.Value(sharedConnKey{}).(*sharedConn); ok {
		conn, err := s.get(ctx)
		if err != nil {
			return nil, nil, err
		}
		if conn != nil {
			return conn, func() {}, nil
		}
		// Released while a read was still in flight. Odd, but answering the
		// question with a connection of our own beats failing over bookkeeping.
	}

	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNoServiceManager, err)
	}
	return conn, func() { conn.Close() }, nil
}

// systemdUnit drives a unit over the system bus.
//
// D-Bus rather than shelling out to systemctl (design.md §8): the properties
// come back typed, there is no output format to parse and re-parse when systemd
// changes it, and job completion is reported rather than inferred from an exit
// code.
type systemdUnit struct {
	unit string
}

// NewUnit returns a Unit for a systemd unit name.
func NewUnit(unit string) (Unit, error) {
	return systemdUnit{unit: unit}, nil
}

// connect opens a system bus connection. One per call: these are short
// operations at human timescale, and a cached connection would have to handle
// systemd restarting underneath it.
func (s systemdUnit) connect(ctx context.Context) (*dbus.Conn, error) {
	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoServiceManager, err)
	}
	return conn, nil
}

// Status is the one read here, and the only method that shares a connection.
// The mutators keep one each: they are not called in a loop, and a request that
// restarts a unit has bigger costs than a handshake.
func (s systemdUnit) Status(ctx context.Context) (UnitStatus, error) {
	conn, release, err := readConn(ctx)
	if err != nil {
		return UnitStatus{Unit: s.unit}, err
	}
	defer release()

	props, err := conn.GetUnitPropertiesContext(ctx, s.unit)
	if err != nil {
		return UnitStatus{Unit: s.unit}, fmt.Errorf("reading %s properties: %w", s.unit, err)
	}

	status := UnitStatus{
		Unit:     s.unit,
		State:    stringProp(props, "ActiveState"),
		SubState: stringProp(props, "SubState"),
		MainPID:  int(uint32Prop(props, "MainPID")),
	}
	status.Active = status.State == "active" || status.State == "reloading"

	// UnitFileState is "enabled", "disabled", "masked", "static", or empty when
	// the unit file is not installed at all — systemd answers for a unit it has
	// never heard of rather than failing, so an absent state is the only signal
	// that the file is missing.
	switch unitFile := stringProp(props, "UnitFileState"); unitFile {
	case "":
		status.Installed = false
	case "enabled", "enabled-runtime":
		status.Installed = true
		status.Enabled = true
	default:
		status.Installed = true
	}

	if ts := uint64Prop(props, "ActiveEnterTimestamp"); ts > 0 {
		status.Since = time.UnixMicro(int64(ts)).UTC()
	}

	return status, nil
}

func (s systemdUnit) Start(ctx context.Context) error   { return s.job(ctx, "start") }
func (s systemdUnit) Stop(ctx context.Context) error    { return s.job(ctx, "stop") }
func (s systemdUnit) Restart(ctx context.Context) error { return s.job(ctx, "restart") }
func (s systemdUnit) Reload(ctx context.Context) error  { return s.job(ctx, "reload") }

// Enable installs the unit's [Install] symlinks so it starts at boot.
//
// Idempotent, and called on every apply rather than only when the state
// changes: it is one D-Bus round trip at human timescale, and the alternative —
// tracking whether we think it is already enabled — is a cache that can be
// wrong in the direction that loses DHCP after a reboot.
func (s systemdUnit) Enable(ctx context.Context) error {
	conn, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// runtime=false writes under /etc so it survives a reboot, which is the
	// entire point. force=true lets an existing symlink be replaced rather than
	// making a re-enable an error.
	if _, _, err := conn.EnableUnitFilesContext(ctx, []string{s.unit}, false, true); err != nil {
		return fmt.Errorf("enabling %s: %w", s.unit, err)
	}
	return s.reloadDaemon(ctx, conn)
}

// Disable removes those symlinks. It does not stop a running unit; the caller
// decides that separately, because "do not come back after a reboot" and "stop
// now" are different requests.
func (s systemdUnit) Disable(ctx context.Context) error {
	conn, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.DisableUnitFilesContext(ctx, []string{s.unit}, false); err != nil {
		return fmt.Errorf("disabling %s: %w", s.unit, err)
	}
	return s.reloadDaemon(ctx, conn)
}

// reloadDaemon is systemd's daemon-reload, so the symlink change is visible to
// systemd rather than only on disk.
//
// Over D-Bus rather than by running systemctl: §3.6's sandbox on olrd is
// affordable precisely because olrd executes no subprocesses, and the first
// exec added would cost most of it.
func (s systemdUnit) reloadDaemon(ctx context.Context, conn *dbus.Conn) error {
	if err := conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("reloading the systemd manager after changing %s: %w", s.unit, err)
	}
	return nil
}

// job runs a unit job and waits for systemd to report how it finished.
//
// Waiting matters: `olr dhcp set` applies on return (design.md §5.1), so
// returning as soon as the job is *queued* would let the command report success
// while the daemon is still failing to start.
func (s systemdUnit) job(ctx context.Context, verb string) error {
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
func (s systemdUnit) hint(ctx context.Context) string {
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
