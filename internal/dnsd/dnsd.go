// Package dnsd is open-linux-router's DNS relay, olr-dnsd.
//
// It owns :53, applies per-client policy, and forwards everything else to
// unbound on loopback. It is a *backend*, not a module: design.md §3.5 gives
// the deciding test — does it have to keep running while olrd is stopped? — and
// DNS does. A control plane that blipped the whole building's name resolution
// on every restart, or took it down with an unrelated panic in an HTTP handler,
// is the risk docs/dns.md §5 spends its length on.
//
// So this process is driven exactly the way dnsmasq is driven: rendered
// configuration files, and a signal. It reads nothing from olrd and calls
// nothing in olrd. Started by hand against a config file, it works.
//
// A package rather than a command since the three binaries were merged: `olr`
// reaches this through `olr internal dns-relay`, which olr-dnsd.service
// invokes. Sharing a binary with olrd is not sharing an address space — the
// two are still separate processes under separate sandboxes, which is the
// whole of what §3.5 asks for.
package dnsd

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/open-linux-router/open-linux-router/internal/buildinfo"
	"github.com/open-linux-router/open-linux-router/internal/dnsrelay"
)

// DefaultConfig is where olrd renders this process's configuration.
const DefaultConfig = "/etc/open-linux-router/rendered/dns/relay.json"

// Main is the relay's entry point, reached as `olr internal dns-relay`.
//
// The prefix on the error is still olr-dnsd rather than olr. One binary now
// carries every role, so the name of the executable no longer says which one
// spoke — and in a journal holding all three, that is the only thing that
// does.
func Main(args []string) int {
	if err := run(args); err != nil {
		fmt.Fprintln(os.Stderr, "olr-dnsd:", err)
		return 1
	}
	return 0
}

func run(args []string) error {
	// A FlagSet of our own, not the package-level CommandLine: `olr` owns
	// os.Args, and this process is reached through a subcommand, so the flags
	// to parse are the ones after it rather than all of them. ExitOnError
	// keeps the behaviour a bad flag used to get from flag.Parse().
	fs := flag.NewFlagSet("olr internal dns-relay", flag.ExitOnError)
	var (
		configPath = fs.String("config", DefaultConfig,
			"rendered relay configuration, written by olrd")
		root = fs.String("root", "",
			"prefix the configuration path with this directory (development only)")
		logLevel = fs.String("log-level", "info", "debug|info|warn|error")
		version  = fs.Bool("version", false, "print version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *version {
		fmt.Println(buildinfo.String())
		return nil
	}

	logger, err := newLogger(*logLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	path := *configPath
	if *root != "" {
		path = *root + path
		logger.Warn("running against a relocated root; this is a development mode", "root", *root)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w\n"+
			"This file is rendered by olrd. If it is missing, DNS has not been configured yet — "+
			"run `olr dns set --listen <address>` and enable the module", path, err)
	}
	cfg, err := dnsrelay.UnmarshalConfig(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	relay, err := dnsrelay.New(cfg, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// SIGHUP re-reads the policy directory and nothing else. That asymmetry is
	// the whole reason olrd renders two files rather than one: rebinding a
	// socket needs a restart, and editing a blocklist — the common operation by
	// a wide margin — must not interrupt a single query.
	go func() {
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if err := relay.Reload(); err != nil {
					// The previous policies stay in force. A half-written file
					// must not silently unblock everything it used to block.
					logger.Error("reload failed; the previous policies are still in force",
						"error", err)
					continue
				}
				logger.Info("reloaded")
			}
		}
	}()

	// Told to systemd only once every socket is bound and the relay is
	// genuinely answering. That is what makes Type=notify load-bearing here
	// rather than decorative: the unit loads the nftables redirect in
	// ExecStartPost, and doing that before this process could answer would
	// point every device's DNS at a closed port.
	ready := func() {
		if _, err := sddaemon.SdNotify(false, sddaemon.SdNotifyReady); err != nil {
			logger.Warn("could not notify systemd of readiness", "error", err)
		}
	}

	if err := relay.Run(ctx, ready); err != nil {
		return err
	}

	logger.Info("shutting down")
	_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyStopping)
	return nil
}

func newLogger(level string) (*slog.Logger, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid --log-level %q (want debug, info, warn or error)", level)
	}
	// Text to stderr: systemd captures it into the journal, which is where
	// design.md §3.4 says logs belong. No log file of our own — and in
	// particular no query log on disk, which is a deliberate choice rather than
	// an omission (see internal/dnsrelay/log.go).
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})), nil
}
