package socksout

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	sddaemon "github.com/coreos/go-systemd/v22/daemon"
	"github.com/open-linux-router/open-linux-router/internal/buildinfo"
	"github.com/vishvananda/netlink"
	"github.com/xjasonlyu/tun2socks/v2/engine"
)

// Main runs in its own process, separate from the control plane. The upstream
// engine is process-global and exits on startup errors, so it must not run in
// olrd's address space.
func Main(args []string) int {
	fs := flag.NewFlagSet("olr internal socks-out", flag.ContinueOnError)
	path := fs.String("config", DefaultConfig, "SOCKS5 outbound configuration")
	version := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "socks-out takes no positional arguments")
		return 2
	}
	if *version {
		fmt.Println(buildinfo.String())
		return 0
	}
	cfg, err := Read(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if !cfg.Enabled {
		fmt.Fprintln(os.Stderr, "outbound SOCKS5 is disabled")
		return 1
	}

	engine.Insert(&engine.Key{Device: "tun://" + Interface, Proxy: cfg.Proxy})
	engine.Start()
	defer engine.Stop()
	link, err := netlink.LinkByName(Interface)
	if err == nil {
		err = netlink.LinkSetUp(link)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bringing up %s: %v\n", Interface, err)
		return 1
	}

	if _, err := sddaemon.SdNotify(false, sddaemon.SdNotifyReady); err != nil {
		fmt.Fprintf(os.Stderr, "notifying systemd: %v\n", err)
		return 1
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	<-stop
	return 0
}
