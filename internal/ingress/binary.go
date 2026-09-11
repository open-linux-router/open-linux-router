package ingress

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Finding the proxy binary the operator supplied.
//
// olr does not ship one. That is the same relationship every other module has
// with its backend — `dhcp` does not ship dnsmasq and `dns` does not ship
// unbound — and it is worth naming why this module was ever going to be the
// exception. No official Caddy package carries DNS providers, because they are
// compiled in rather than loaded at runtime, and DNS-01 is the only challenge
// that works for a name which does not resolve from the internet
// (docs/ingress.md §4). So somebody has to produce a binary with the right
// module in it.
//
// Having that somebody be the operator costs them a download and costs us
// nothing we were able to afford: shipping a build would have made every Caddy
// vulnerability ours to chase, and would have frozen the provider list at
// whatever we linked. It also puts the module back in line with the rest of
// olr, which drives backends and packages none of them.
//
// What we owe in exchange is that every failure along this path says exactly
// what is missing and exactly how to get it. That is the whole of this file.

// SearchPath is where a proxy binary is looked for, in order.
//
// The first entry is the one documented and the one to prefer: a binary olr
// found there is unambiguously the one an operator put there for olr. `caddy`
// on PATH is a fallback that will usually be the distro's package — which works
// for everything except obtaining a certificate, and fails at that with a clear
// message from Caddy itself rather than from a guess of ours.
var SearchPath = []string{
	"/usr/lib/open-linux-router/caddy",
}

// ErrNoBinary reports that no proxy binary could be found.
var ErrNoBinary = errors.New("no proxy binary found")

// FindBinary returns the proxy binary to use, or ErrNoBinary.
func FindBinary() (string, error) {
	for _, path := range SearchPath {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return path, nil
	}
	if path, err := exec.LookPath("caddy"); err == nil {
		return path, nil
	}
	return "", ErrNoBinary
}

// ErrBinaryMissing explains how to satisfy FindBinary.
//
// Long, and deliberately so: this is the message standing between an operator
// and the feature, and every sentence of it removes a search they would
// otherwise have to do. The download page matters most — it produces a binary
// with the providers ticked and needs no Go toolchain, which is the difference
// between a five-minute step and an afternoon.
func ErrBinaryMissing() error {
	return fmt.Errorf(
		"no proxy binary found at %s, and no `caddy` on PATH.\n\n"+
			"olr does not ship one, because publishing an internal name over HTTPS needs a\n"+
			"Caddy built with your DNS provider's module and no official package carries\n"+
			"those — they are compiled in, not loaded at runtime.\n\n"+
			"Get one either way:\n"+
			"  • https://caddyserver.com/download — tick your DNS provider, download, no\n"+
			"    Go toolchain needed; or\n"+
			"  • xcaddy build --with github.com/caddy-dns/<your provider>\n\n"+
			"Then put it at %s and make it executable.\n"+
			"`olr ingress show providers` will tell you what it ended up with",
		SearchPath[0], SearchPath[0])
}
