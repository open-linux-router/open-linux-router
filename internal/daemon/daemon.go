// Package daemon is the open-linux-router control plane, olrd.
//
// It is one resident process holding the route table, the schema, the config
// store and the apply lock (design.md §3.5). Modules live inside it; backends
// — dnsmasq and everything like it — do not, and never will. The invariant that
// decides that split is worth repeating at the top of the package it governs:
//
//	systemctl restart olrd must never drop a packet, expire a lease, or break
//	a session.
//
// A package rather than a command since the three binaries were merged: `olr`
// reaches this through `olr internal daemon`, which olrd.service invokes. The
// process is still its own, which is the only part §3.5 was ever about.
package daemon

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/open-linux-router/open-linux-router/internal/buildinfo"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/mcp"
	"github.com/open-linux-router/open-linux-router/internal/gateway"
	"github.com/open-linux-router/open-linux-router/internal/webui"
)

type options struct {
	socket    string
	listen    string
	links     string
	root      string
	tokenPath string
	noAuth    bool
	logLevel  string
	version   bool
}

// Main is the control plane's entry point, reached as `olr internal daemon`.
//
// The prefix on the error is still olrd rather than olr. One binary now
// carries every role, so the name of the executable no longer says which one
// spoke — and in a journal holding all three, that is the only thing that
// does.
func Main(args []string) int {
	if err := run(args); err != nil {
		fmt.Fprintln(os.Stderr, "olrd:", err)
		return 1
	}
	return 0
}

func run(args []string) error {
	var opts options

	// A FlagSet of our own, not the package-level CommandLine: `olr` owns
	// os.Args, and this process is reached through a subcommand, so the flags
	// to parse are the ones after it rather than all of them. ExitOnError
	// keeps the behaviour a bad flag used to get from flag.Parse().
	fs := flag.NewFlagSet("olr internal daemon", flag.ExitOnError)
	fs.StringVar(&opts.socket, "socket", core.DefaultSocket,
		"unix socket to serve the API on")
	fs.StringVar(&opts.listen, "listen", "",
		"additional TCP address for the WebUI and remote clients, e.g. 127.0.0.1:8080 (off by default)")
	fs.StringVar(&opts.links, "links", "",
		"read interface facts from this JSON file instead of the kernel (development only)")
	fs.StringVar(&opts.root, "root", "",
		"prefix every configuration and state path with this directory (development only)")
	fs.StringVar(&opts.tokenPath, "token-file", core.TokenPath,
		"file holding the API token for the TCP listener")
	fs.BoolVar(&opts.noAuth, "no-auth", false,
		"serve the TCP listener without a token (loopback addresses only)")
	fs.StringVar(&opts.logLevel, "log-level", "info", "debug|info|warn|error")
	fs.BoolVar(&opts.version, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if opts.version {
		fmt.Println(buildinfo.String())
		return nil
	}

	logger, err := newLogger(opts.logLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	// --- modules ----------------------------------------------------------
	//
	// Mounted as a literal list. The set is bounded and known at compile time,
	// so there is no registry and no Module interface to satisfy (§3.1/§3.2).

	source, err := interfaceSource(opts.links)
	if err != nil {
		return err
	}

	// One store for the whole box (core.ConfigPath). The module list is given
	// literally here for the same reason the mounts below are: the set is
	// bounded and known at compile time (§3.2), and the order it is given in is
	// the order the document is written in.
	// `link` comes first because everything else reads it: an interface has to
	// have been handed over before a pool, a resolver or an exit may name it.
	// `dhcp` then `dns` is §3.2's own literal list, and it is also the order
	// they matter in on a box being brought up: addresses first, then names.
	// `devices` follows both rather than leading them. Its identity half is a
	// foundation object that `firewall` and `qos` will reference (§4.4), but its
	// presence half reads the lease database through `dhcp`, so it is the last
	// of the three to come up.
	// `gateway` sits after them all, because an exit is only useful once
	// clients have addresses and names — and because docs/gateway.md §4 has its
	// domain half depending on `dns` owning :53, not the other way round.
	store := core.NewStore(core.RootedConfigPath(opts.root),
		link.ModuleName, dhcp.ModuleName, dns.ModuleName, devices.ModuleName, gateway.ModuleName)
	checkStore(store, logger)

	// The three consumers' windows onto link, all backed by one Facts: the
	// kernel for addresses and state, the document for adoption. Reading both
	// per request is what keeps §4.5 true — there is no cached copy of either
	// to go stale while olrd is running.
	facts := link.Facts{Source: source, Store: store}
	links := dhcpLinkView{facts: facts}
	dnsLinks := dnsLinkView{facts: facts}
	gatewayLinks := gatewayLinkView{facts: facts}

	applier, err := dhcp.NewApplierAt(store, links, opts.root)
	if err != nil {
		return fmt.Errorf("initialising dhcp: %w", err)
	}
	dnsApplier, err := dns.NewApplierAt(store, dnsLinks, dnsReservations{applier: applier}, opts.root)
	if err != nil {
		return fmt.Errorf("initialising dns: %w", err)
	}
	if opts.root != "" {
		logger.Warn("running against a relocated root; this is a development mode",
			"root", opts.root)
	}

	srv := core.New()

	// `link` is mounted first, matching the store's order and the order a box
	// is brought up in. It is the only module here that drives nothing: adopting
	// an interface writes a line of consent and touches neither the kernel nor a
	// daemon, which is precisely what lets §7 promise that installing olr
	// changes nothing until you say so.
	srv.Mount(link.ModuleName, link.HTTP{
		Applier: link.Applier{Store: store, Source: source},
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), link.Config{})

	srv.Mount(dhcp.ModuleName, dhcp.HTTP{
		Applier: applier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), dhcp.Config{})

	srv.Mount(dns.ModuleName, dns.HTTP{
		Applier: dnsApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Routes(), dns.Config{})

	srv.Mount(devices.ModuleName, devices.HTTP{
		Applier: devices.Applier{
			Store: store,
			// Two presence sources, and the pair is the point: leases know
			// about anything that asked for an address, ARP sees the
			// statically-addressed printer that never did (§10 decision 7).
			Presence: []devices.PresenceSource{
				dhcpPresence{applier: applier},
				devices.ARP{},
			},
			Fixed:    dhcpFixedAddresses{applier: applier},
			Networks: dhcpNetworks{applier: applier},
		},
		Lock:   srv.ApplyLock(),
		Events: srv.Events(),
	}.Routes(), devices.Config{})

	// `gateway` is the one module whose configuration lives in the kernel
	// rather than in a file some backend reads, so two things follow that the
	// others do not need: it is applied at startup (below), and a background
	// prober can change what the kernel should hold without the operator
	// touching anything.
	prober := gateway.NewProber()
	prober.Log = logger
	gatewayApplier := gateway.Applier{
		Kernel: gateway.NewKernel(),
		Links:  gatewayLinks,
		Store:  store,
		Probes: prober,
	}
	prober.OnChange = func(exit string, up bool) {
		// An exit changed state, so the routing the kernel should hold has
		// changed with it — a dead exit's traffic goes to `unreachable`, and a
		// recovered one gets its route back. Re-applying is how that lands,
		// and it goes through the same global apply lock as an operator's edit
		// (§3.6) so the two can never interleave.
		reapplyGateway(gatewayApplier, srv, logger, exit, up)
	}

	srv.Mount(gateway.ModuleName, gateway.HTTP{
		Applier: gatewayApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
		Watch:   func(cfg gateway.Config) { prober.Watch(context.Background(), cfg) },
	}.Routes(), gateway.Config{})

	// --- routes -----------------------------------------------------------
	//
	// The API and the SPA are composed here rather than inside core, which has
	// no business knowing a UI exists. ServeMux prefers the longer pattern, so
	// /api/ wins over / without any ordering subtlety.

	top := http.NewServeMux()
	top.Handle(core.APIPrefix+"/", srv.Handler())
	top.Handle("/", webui.Handler())

	// The MCP surface (§6.4), composed here for the same reason the SPA is:
	// core has no business knowing an agent exists, and mcp.New takes the API
	// handler rather than the server so that it cannot reach past it.
	//
	// It is mounted under /api on purpose. ServeMux prefers the more specific
	// pattern, so this wins over /api/ above with no ordering subtlety, and
	// sitting inside the prefix means it inherits the authentication both
	// listeners already apply — the token on TCP, the socket's mode locally.
	// An MCP endpoint outside /api would be an unauthenticated admin surface.
	mcpServer, err := mcp.New(srv.Handler())
	if err != nil {
		return err
	}
	top.Handle(core.APIPrefix+"/mcp", mcpServer)
	logger.Info("serving MCP", "path", core.APIPrefix+"/mcp", "tools", len(mcpServer.Tools()))

	handler := core.WithLogging(core.WithRecovery(top))

	// --- listeners --------------------------------------------------------

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Routing is put back into the kernel here, and it is the only module that
	// needs this.
	//
	// dnsmasq and unbound read files that survive a reboot; nftables rules,
	// `ip rule` entries and route tables do not, so without this a box would
	// come back up with its configuration intact and none of it in force. It is
	// idempotent by construction — the plan against an already-correct kernel
	// is empty and nothing is written — which is what keeps design.md §3.5's
	// invariant true: `systemctl restart olrd` re-runs this and disturbs no
	// traffic.
	//
	// It never fails the start. A box whose routing cannot be programmed is
	// exactly the box whose API has to come up, because the API is how it gets
	// fixed.
	startGateway(ctx, gatewayApplier, prober, logger)

	var listeners []net.Listener

	unix, err := core.ListenUnix(opts.socket)
	if err != nil {
		return err
	}
	listeners = append(listeners, unix)
	logger.Info("listening", "socket", opts.socket, "auth", "socket permissions")

	if opts.listen != "" {
		tcp, authed, err := tcpListener(opts, handler, logger)
		if err != nil {
			unix.Close()
			return err
		}
		listeners = append(listeners, tcp.listener)
		logger.Info("listening", "address", opts.listen, "auth", authed)

		// Two servers rather than one, because they do not share a handler:
		// the socket is authenticated by its file mode, the TCP listener by a
		// token. Wrapping both in the token check would break `olr` over the
		// socket for no gain.
		go serve(tcp.server, tcp.listener, logger)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: it would cut off /api/events, which is a long-lived
		// stream by design (§6.3).
	}
	go serve(server, unix, logger)

	// Told to systemd only once every listener is bound, which is what makes
	// Type=notify worth having: `olr start` returns when the API is
	// actually answering, not when the process was forked. Outside systemd
	// NOTIFY_SOCKET is unset and this is a no-op.
	if _, err := sddaemon.SdNotify(false, sddaemon.SdNotifyReady); err != nil {
		logger.Warn("could not notify systemd of readiness", "error", err)
	}

	<-ctx.Done()
	logger.Info("shutting down")
	_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyStopping)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = server.Shutdown(shutdownCtx)
	for _, l := range listeners {
		l.Close()
	}
	return err
}

type tcpServer struct {
	server   *http.Server
	listener net.Listener
}

// tcpListener builds the TCP half, which is the only surface that needs
// authenticating.
func tcpListener(opts options, handler http.Handler, logger *slog.Logger) (tcpServer, string, error) {
	authed := "bearer token"

	if opts.noAuth {
		// The one guard that makes --no-auth defensible: it cannot be reached
		// from the network. An unauthenticated admin API on a router's LAN
		// address is not a development convenience, it is a vulnerability.
		if !core.IsLoopback(opts.listen) {
			return tcpServer{}, "", fmt.Errorf(
				"--no-auth requires a loopback --listen address, got %q", opts.listen)
		}
		logger.Warn("serving without authentication", "address", opts.listen)
		authed = "none"
	} else {
		token, err := core.LoadOrCreateToken(opts.tokenPath)
		if err != nil {
			return tcpServer{}, "", err
		}
		handler = core.BearerAuth(token, handler)
	}

	l, err := core.ListenTCP(opts.listen)
	if err != nil {
		return tcpServer{}, "", err
	}
	return tcpServer{
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
		listener: l,
	}, authed, nil
}

func serve(s *http.Server, l net.Listener, logger *slog.Logger) {
	if err := s.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
	}
}

// checkStore reads the configuration document once at startup, to say something
// about it while there is still an operator watching.
//
// It never fails the start. A box whose config file is corrupt is exactly the
// box whose API has to come up, because the API is how it gets fixed — and each
// request reports the parse error again on its own. What this buys is that the
// error is in the journal at boot rather than only in the reply to whoever asks
// first.
func checkStore(store *core.Store, logger *slog.Logger) {
	doc, err := store.Load()
	if err != nil {
		logger.Error("configuration could not be read; the API will report this on every request",
			"path", store.Path(), "error", err)
		return
	}
	if unknown := doc.Unknown(); len(unknown) > 0 {
		// Preserved, not dropped: this is what an older olr sees after a newer
		// one has written a module it does not have. Saying so beats silence,
		// because the alternative reading — "my config vanished" — is the one
		// an operator would otherwise reach for.
		logger.Warn("configuration contains sections for modules this build does not have; they are preserved untouched",
			"path", store.Path(), "sections", strings.Join(unknown, ", "))
	}
	if legacy := store.LegacyPaths(); len(legacy) > 0 {
		logger.Warn("per-module configuration files found; they are read only when the document is absent and can be deleted once it exists",
			"document", store.Path(), "files", strings.Join(legacy, ", "))
	}
}

// startGateway programs stored gateway intent and starts the health probes.
func startGateway(ctx context.Context, a gateway.Applier, prober *gateway.Prober, logger *slog.Logger) {
	cfg, err := a.Load()
	if err != nil {
		logger.Error("gateway configuration could not be read; nothing was programmed",
			"error", err)
		return
	}
	if cfg.Empty() && !cfg.Enabled {
		return
	}

	result, _, err := a.Apply(ctx, cfg, netip.Addr{})
	switch {
	case err != nil && result.Plan.Blocked != "":
		// §6's refusal, which is the one failure here an operator can act on
		// directly — and the one where saying nothing would leave them
		// wondering why their exits do nothing.
		logger.Error("gateway was not applied because something else is managing it",
			"reason", result.Plan.Blocked)
	case err != nil:
		logger.Error("gateway could not be applied", "error", err,
			"steps", len(result.Steps))
	case !result.Plan.Empty():
		logger.Info("gateway applied", "changes", len(result.Plan.Changes))
	}

	prober.Watch(ctx, cfg)
}

// reapplyGateway re-programs the kernel after an exit changed health.
func reapplyGateway(a gateway.Applier, srv *core.Server, logger *slog.Logger, exit string, up bool) {
	// Bounded, because it runs under the global apply lock and design.md §3.6
	// requires apply to be bounded rather than to wait for convergence.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := srv.ApplyLock().Do(ctx, func() error {
		cfg, err := a.Load()
		if err != nil {
			return err
		}
		_, _, err = a.Apply(ctx, cfg, netip.Addr{})
		return err
	})
	if err != nil {
		logger.Error("could not re-route after an exit changed state",
			"exit", exit, "up", up, "error", err)
		return
	}

	// Announced so the UI re-reads. The device that just lost its internet is
	// on somebody's screen, and "no internet — Clash is down" is only useful if
	// it appears without a refresh.
	srv.Events().Publish(core.Event{Type: core.EventApplied, Module: gateway.ModuleName})
}

func newLogger(level string) (*slog.Logger, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid --log-level %q (want debug, info, warn or error)", level)
	}
	// Text to stderr: systemd captures it into the journal, which is where
	// §3.4 says logs belong. No log file of our own.
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})), nil
}
