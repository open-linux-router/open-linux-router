// Command olrd is the open-linux-router control plane.
//
// It is one resident process holding the route table, the schema, the config
// store and the apply lock (design.md §3.5). Modules live inside it; backends
// — dnsmasq and everything like it — do not, and never will. The invariant that
// decides that split is worth repeating at the top of the binary it governs:
//
//	systemctl restart olrd must never drop a packet, expire a lease, or break
//	a session.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "olrd:", err)
		os.Exit(1)
	}
}

func run() error {
	var opts options

	flag.StringVar(&opts.socket, "socket", core.DefaultSocket,
		"unix socket to serve the API on")
	flag.StringVar(&opts.listen, "listen", "",
		"additional TCP address for the WebUI and remote clients, e.g. 127.0.0.1:8080 (off by default)")
	flag.StringVar(&opts.links, "links", "",
		"JSON file of interface facts, until the link module lands")
	flag.StringVar(&opts.root, "root", "",
		"prefix every configuration and state path with this directory (development only)")
	flag.StringVar(&opts.tokenPath, "token-file", core.TokenPath,
		"file holding the API token for the TCP listener")
	flag.BoolVar(&opts.noAuth, "no-auth", false,
		"serve the TCP listener without a token (loopback addresses only)")
	flag.StringVar(&opts.logLevel, "log-level", "info", "debug|info|warn|error")
	flag.BoolVar(&opts.version, "version", false, "print version and exit")
	flag.Parse()

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

	links, err := loadLinks(opts.links)
	if err != nil {
		return err
	}
	dnsLinks, err := loadDNSLinks(opts.links)
	if err != nil {
		return err
	}

	// One store for the whole box (core.ConfigPath). The module list is given
	// literally here for the same reason the mounts below are: the set is
	// bounded and known at compile time (§3.2), and the order it is given in is
	// the order the document is written in.
	// `dhcp` then `dns` is §3.2's own literal list, and it is also the order
	// they matter in on a box being brought up: addresses first, then names.
	// `devices` follows both rather than leading them. Its identity half is a
	// foundation object that `firewall` and `qos` will reference (§4.4), but its
	// presence half reads the lease database through `dhcp`, so it is the last
	// of the three to come up.
	store := core.NewStore(core.RootedConfigPath(opts.root),
		dhcp.ModuleName, dns.ModuleName, devices.ModuleName)
	checkStore(store, logger)

	applier, err := dhcp.NewApplierAt(store, links, opts.root)
	if err != nil {
		return fmt.Errorf("initialising dhcp: %w", err)
	}
	dnsApplier, err := dns.NewApplierAt(store, dnsLinks, opts.root)
	if err != nil {
		return fmt.Errorf("initialising dns: %w", err)
	}
	if opts.root != "" {
		logger.Warn("running against a relocated root; this is a development mode",
			"root", opts.root)
	}

	srv := core.New()
	srv.Mount(dhcp.ModuleName, dhcp.HTTP{
		Applier: applier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Handler(), dhcp.Config{})

	srv.Mount(dns.ModuleName, dns.HTTP{
		Applier: dnsApplier,
		Lock:    srv.ApplyLock(),
		Events:  srv.Events(),
	}.Handler(), dns.Config{})

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
			Fixed: dhcpFixedAddresses{applier: applier},
		},
		Lock:   srv.ApplyLock(),
		Events: srv.Events(),
	}.Handler(), devices.Config{})

	// --- routes -----------------------------------------------------------
	//
	// The API and the SPA are composed here rather than inside core, which has
	// no business knowing a UI exists. ServeMux prefers the longer pattern, so
	// /api/ wins over / without any ordering subtlety.

	top := http.NewServeMux()
	top.Handle(core.APIPrefix+"/", srv.Handler())
	top.Handle("/", webui.Handler())

	handler := core.WithLogging(core.WithRecovery(top))

	// --- listeners --------------------------------------------------------

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	// Type=notify worth having: `olr daemon start` returns when the API is
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

// loadLinks reads the interface facts the dhcp module validates pools against.
//
// A stand-in until the link module lands (§9 milestone 1). Pools are keyed by
// kernel interface name here; design.md §4.4 keys them by group, so this whole
// path changes shape when link arrives — it is scaffolding, not a design.
func loadLinks(path string) (dhcp.LinkView, error) {
	if path == "" {
		// No facts means validation cannot check a pool against its subnet.
		// Empty rather than fatal so olrd starts on a box with no config yet.
		return dhcp.StaticLinks{}, nil
	}
	links, err := dhcp.LoadLinks(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return links, nil
}

// loadDNSLinks reads the same file again, into the dns module's own view.
//
// Twice, rather than once into a shared type, and deliberately: design.md §4.1
// has each consumer declare the facts it needs, and the two modules do not need
// the same ones — dhcp names an interface and asks about it, dns names an
// address and has to find which interface owns it. Both of these stand-ins are
// deleted the day the link module lands and satisfies both interfaces directly,
// so a shared abstraction here would be one built for a pair of things with a
// known expiry date.
func loadDNSLinks(path string) (dns.LinkView, error) {
	if path == "" {
		return dns.StaticLinks{}, nil
	}
	links, err := dns.LoadLinks(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return links, nil
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
