//go:build linux

package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// External speed tools create their own sockets, so the way out cannot be a
// Setsockopt on a connection this process owns. Instead the child gets a
// reserved group id and nftables marks every socket that group opens, which is
// the same mark the routing policy already matches. internal/inspection uses
// this for mitmweb; the group is never created in /etc/group, and the value
// sits above the range a real system group would use.
const (
	speedGID   = 2147418114 // 0x7fff0002, one past inspection's reserved gid
	speedTable = "olr_speed"
)

// nft and euid are variables so tests can record rules and run unprivileged.
var euid = os.Geteuid

var nft = func(args ...string) error {
	out, err := exec.Command("nft", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// markCommand makes cmd's sockets select the way out whose fwmark is mark. It
// returns the cleanup that removes the rule; a zero mark is the router's own
// default route, which needs no rule and no group.
//
// The child has to start after the rule exists, so callers prepare the command
// once and start it themselves.
func markCommand(cmd *exec.Cmd, mark uint32) (func(), error) {
	noop := func() {}
	if mark == 0 {
		return noop, nil
	}
	if euid() != 0 {
		return nil, errors.New("a way out for an external speed tool needs root, because marking its sockets writes an nftables rule")
	}
	// A run that was killed before its cleanup left the table behind. The rule
	// can only ever match the reserved group, so replacing the table is safe.
	_ = nft("delete", "table", "inet", speedTable)
	if err := nft("add", "table", "inet", speedTable); err != nil {
		return nil, err
	}
	cleanup := func() { _ = nft("delete", "table", "inet", speedTable) }
	if err := nft("add", "chain", "inet", speedTable, "output",
		"{ type route hook output priority mangle; policy accept; }"); err != nil {
		cleanup()
		return nil, err
	}
	if err := nft("add", "rule", "inet", speedTable, "output",
		"meta", "skgid", fmt.Sprint(speedGID), "meta", "mark", "set", fmt.Sprintf("%#x", mark)); err != nil {
		cleanup()
		return nil, err
	}
	// Uid stays 0 because the daemon runs the tool the same way it runs every
	// other helper; only the group changes, and NoSetGroups keeps the child
	// from needing a group database that a router image may not have.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: speedGID, NoSetGroups: true},
	}
	return cleanup, nil
}
