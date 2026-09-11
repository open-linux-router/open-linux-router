//go:build linux

package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"golang.org/x/sys/unix"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The kernel half, and the only file in this module that knows what netlink is.
//
// Everything above it works in values: Render decides, BuildPlan compares, and
// this translates. That is what design.md §10 asks for under test hardware, and
// it is load-bearing here in a way it is not for the file-rendering modules —
// what this module configures *is* the kernel, so without the seam there would
// be nothing testable off a router at all.
//
// There is no `exec.Command` here and there never may be (design.md §3.6): the
// sandbox in olrd.service is nearly free precisely because the process spawns
// nothing, and the first shell-out silently costs all of it.

// LinuxKernel programs the NAT table.
type LinuxKernel struct{}

// NewKernel returns the kernel implementation for this platform.
func NewKernel() Kernel { return LinuxKernel{} }

// --- observation ----------------------------------------------------------

// Observe reads the live state into the same canonical lines Render produces.
//
// Every part is read fresh from the kernel. Nothing here consults a cache of
// what we last wrote, which is what makes drift detection mean something: a
// hand-run `nft delete rule` has to show up the same way a hand-edited config
// file does in the other modules (design.md §5.4, §4.5).
func (k LinuxKernel) Observe(_ context.Context) (Observed, error) {
	obs := Observed{Known: true, Counters: map[string]Counter{}}

	conn, err := nftables.New()
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	defer conn.CloseLasting()

	lines, counters, err := observeTable(conn)
	if err != nil {
		return Observed{}, fmt.Errorf("reading nftables: %w", err)
	}
	obs.Lines = lines
	obs.Counters = counters

	foreign, err := observeForeign(conn)
	if err != nil {
		return Observed{}, fmt.Errorf("reading other nftables tables: %w", err)
	}
	obs.Foreign = foreign

	obs.Listening = observeListening()

	return obs, nil
}

// observeTable reads our table back.
//
// Each rule carries its own canonical line in netlink userdata, in nft's comment
// format, so this is a read rather than a reconstruction — and `nft list
// ruleset` shows the same text `olr diff` does, which is worth more than it
// costs.
//
// **The known gap, stated rather than left to be found.** A rule whose
// expressions were changed by hand while its comment was left intact reads back
// as unchanged, so that specific edit is not detected as drift. Reconstructing a
// rule from its expression list is what would close it, and that is the nftables
// reader design.md §4.2 gives this module for the whole box — a thing it owes
// and has not built. The cost is bounded in the meantime: every apply replaces
// the table wholesale in one transaction, so a hand-edit is *corrected* on the
// next apply even while it goes unreported.
func observeTable(conn *nftables.Conn) ([]string, map[string]Counter, error) {
	counters := map[string]Counter{}

	table, err := ourTable(conn)
	if err != nil || table == nil {
		return nil, counters, err
	}

	out := []string{fmt.Sprintf("nft table inet %s", TableName)}

	objs, err := conn.GetObjects(table)
	if err != nil {
		return nil, nil, err
	}
	for _, o := range objs {
		c, ok := o.(*nftables.CounterObj)
		if !ok {
			continue
		}
		out = append(out, fmt.Sprintf("nft counter %s", c.Name))
		counters[c.Name] = Counter{Packets: c.Packets, Bytes: c.Bytes}
	}

	chains, err := conn.ListChainsOfTableFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range chains {
		if c.Table == nil || c.Table.Name != TableName {
			continue
		}
		switch c.Name {
		case PreroutingChain:
			out = append(out, fmt.Sprintf("nft chain %s prerouting dstnat", PreroutingChain))
		case PostroutingChain:
			out = append(out, fmt.Sprintf("nft chain %s postrouting srcnat", PostroutingChain))
		default:
			continue
		}

		rules, err := conn.GetRules(table, c)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rules {
			if line, ok := userdata.GetString(r.UserData, userdata.TypeComment); ok {
				out = append(out, line)
			} else {
				// A rule in our table that we did not label. Reported as an
				// unrecognised line so it shows up as something to remove,
				// rather than being silently tolerated in a table we claim to
				// own outright.
				out = append(out, fmt.Sprintf("nft unrecognised rule in %s", c.Name))
			}
		}
	}

	return out, counters, nil
}

func ourTable(conn *nftables.Conn) (*nftables.Table, error) {
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		if t.Name == TableName {
			return t, nil
		}
	}
	return nil, nil
}

// observeForeign finds chains on the forward hook, outside our table, whose
// policy could stop a forwarded packet.
//
// docs/firewall.md §5.2: in nftables a drop is final, so an accept in our table
// cannot override a drop in somebody else's. This is not something we can fix,
// and pretending otherwise would be worse than saying nothing — so it is
// detected and reported, and the plan phrases it as *may* because the foreign
// chain could have an accept rule for exactly this traffic that we cannot
// evaluate.
//
// Only `policy drop` chains are collected. An `accept` policy composes with ours
// harmlessly, and reporting every forward chain on a box running Docker would be
// noise on every plan for the life of the installation.
func observeForeign(conn *nftables.Conn) ([]ForeignFilter, error) {
	var out []ForeignFilter

	// Every family, because a filter can live in any of them and `inet` is not
	// where a distribution's own ruleset usually sits.
	families := []nftables.TableFamily{
		nftables.TableFamilyINet,
		nftables.TableFamilyIPv4,
		nftables.TableFamilyIPv6,
		nftables.TableFamilyBridge,
	}
	for _, fam := range families {
		chains, err := conn.ListChainsOfTableFamily(fam)
		if err != nil {
			return nil, err
		}
		for _, c := range chains {
			if c.Table == nil || c.Table.Name == TableName {
				continue
			}
			if c.Hooknum == nil || *c.Hooknum != *nftables.ChainHookForward {
				continue
			}
			if c.Policy == nil || *c.Policy != nftables.ChainPolicyDrop {
				continue
			}
			out = append(out, ForeignFilter{
				Table:  c.Table.Name,
				Family: familyName(fam),
				Chain:  c.Name,
				Policy: "drop",
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Chain < out[j].Chain
	})
	return out, nil
}

func familyName(f nftables.TableFamily) string {
	switch f {
	case nftables.TableFamilyINet:
		return "inet"
	case nftables.TableFamilyIPv4:
		return "ip"
	case nftables.TableFamilyIPv6:
		return "ip6"
	case nftables.TableFamilyBridge:
		return "bridge"
	}
	return fmt.Sprintf("family(%d)", int(f))
}

// observeListening reads what this box is already serving on its own ports.
//
// Through core rather than by parsing procfs again: internal/dhcp's preflight
// already needed this file, and one parser for a fixed hex format is one place
// for it to be wrong.
//
// A failure is swallowed rather than failing the observation. What this feeds is
// a warning (docs/firewall.md §5.2), and a box whose procfs we cannot read is
// not a box whose port forwards should stop being manageable.
func observeListening() []ListeningPort {
	var out []ListeningPort
	if ports, err := core.ListeningTCPPorts(); err == nil {
		for _, p := range ports {
			out = append(out, ListeningPort{Protocol: ProtocolTCP, Port: p})
		}
	}
	if ports, err := core.ListeningUDPPorts(); err == nil {
		for _, p := range ports {
			out = append(out, ListeningPort{Protocol: ProtocolUDP, Port: p})
		}
	}
	return out
}

// --- apply ----------------------------------------------------------------

// Apply programs the desired state.
//
// One step, because this module's whole kernel footprint is one nftables table
// and a table is replaced in one atomic batch. internal/gateway needs five
// ordered steps because it spans three namespaces that have to be built
// bottom-up and torn down top-down; there is no equivalent ordering hazard here,
// and inventing extra steps to look similar would be reporting structure that
// does not exist.
func (k LinuxKernel) Apply(_ context.Context, d Desired) ([]Step, error) {
	const desc = "write nftables"
	if err := applyNFT(d); err != nil {
		return []Step{{Description: desc, Error: err.Error()}}, err
	}
	return []Step{{Description: desc, Done: true}}, nil
}

// applyNFT replaces our table wholesale, in one transaction.
//
// Delete-then-recreate rather than a rule-by-rule reconciliation, and the reason
// is that the batch is atomic: no half-built ruleset is ever visible, so there
// is no ordering problem to solve inside it and no window in which a connection
// is matched by half the new rules. It also means a hand-edit inside our table
// is corrected on every apply, which is what bounds the observation gap
// documented on observeTable.
//
// The counters are the one thing that does *not* survive it, and that is the
// declared cost of this shape: rebuilding the table resets every forward's
// count. It is the same event as a reboot, which the counters do not survive
// either, and it is what buys the atomicity above.
//
// It never touches anything else. `nft flush ruleset` is banned outright
// (design.md §3.4) — it silently kills Docker, podman, libvirt and k8s
// networking — so this deletes and recreates one table, by name.
func applyNFT(d Desired) error {
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	defer conn.CloseLasting()

	existing, err := ourTable(conn)
	if err != nil {
		return err
	}
	if existing != nil {
		conn.DelTable(existing)
	}

	if !d.Enabled {
		return conn.Flush()
	}

	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   TableName,
	})

	for _, name := range d.Table.Counters {
		conn.AddObject(&nftables.CounterObj{Table: table, Name: name})
	}

	policy := nftables.ChainPolicyAccept
	pre := conn.AddChain(&nftables.Chain{
		Name:     PreroutingChain,
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
		Policy:   &policy,
	})

	// Rules are added in evaluation order, which is the order Desired.Lines
	// prints them in — so following a packet down `nft list ruleset` and
	// following it down `olr diff` are the same exercise.
	for _, r := range d.Table.DNAT {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: pre,
			Exprs:    dnatExprs(r.In, r.Protocol, r.Port, r.To, r.ToPorts, r.Counter),
			UserData: comment(r.Line()),
		})
	}
	for _, r := range d.Table.Hairpin {
		conn.AddRule(&nftables.Rule{
			Table: table, Chain: pre,
			Exprs:    dnatExprs(r.In, r.Protocol, r.Port, r.To, r.ToPorts, ""),
			UserData: comment(r.Line()),
		})
	}

	if len(d.Table.Masq) > 0 {
		natPolicy := nftables.ChainPolicyAccept
		post := conn.AddChain(&nftables.Chain{
			Name:     PostroutingChain,
			Table:    table,
			Type:     nftables.ChainTypeNAT,
			Hooknum:  nftables.ChainHookPostrouting,
			Priority: nftables.ChainPriorityNATSource,
			Policy:   &natPolicy,
		})
		for _, r := range d.Table.Masq {
			conn.AddRule(&nftables.Rule{
				Table: table, Chain: post,
				Exprs:    masqExprs(r),
				UserData: comment(r.Line()),
			})
		}
	}

	return conn.Flush()
}

// comment stores a line in nft's own comment format, so `nft list ruleset`
// prints it rather than showing an opaque blob.
func comment(s string) []byte {
	return userdata.AppendString(nil, userdata.TypeComment, s)
}

// dnatExprs builds one translation rule.
//
// The guards, in the order the kernel evaluates them and each earning its place:
//
//   - `iifname` scopes the rule to the interface the operator named. A forward
//     that applied to every interface would be a policy nobody wrote, and on a
//     box with two uplinks it would be a surprising one.
//   - `meta nfproto ipv4` is required because this is an `inet` table, where an
//     IPv4 payload offset applied to an IPv6 packet reads whatever happens to be
//     at that offset in the v6 header. internal/gateway's sourceExprs carries
//     the same guard for the same reason.
//   - `fib daddr type local` is what makes this mean *addressed to this router*
//     without naming an address that DHCP or PPPoE will change under us
//     (docs/firewall.md §3.2). It also keeps the rule from capturing transit
//     traffic that happens to use the same port.
//   - the l4proto check, then the destination port.
//
// counter is empty for a hairpin rule, which is the one difference between the
// two — see HairpinRule for why the inside-facing copy deliberately does not
// count.
func dnatExprs(in string, proto Protocol, ports PortRange, to netip.Addr, toPorts PortRange, counter string) []expr.Any {
	out := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: nulTerminated(in)},

		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.NFPROTO_IPV4}},

		&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1,
			Data: binaryutil.NativeEndian.PutUint32(unix.RTN_LOCAL)},

		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{l4proto(proto)}},
	}

	out = append(out, dportExprs(ports)...)

	if counter != "" {
		out = append(out, &expr.Objref{Type: unix.NFT_OBJECT_COUNTER, Name: counter})
	}

	// The address goes in register 1 and the ports in 2 and 3. Two registers
	// for the ports even when the range is a single port: the kernel reads
	// RegProtoMax only when it is set, and setting both to the same value is
	// how a single port is spelled at this layer.
	out = append(out,
		&expr.Immediate{Register: 1, Data: to.AsSlice()},
		&expr.Immediate{Register: 2, Data: binaryutil.BigEndian.PutUint16(toPorts.From)},
		&expr.Immediate{Register: 3, Data: binaryutil.BigEndian.PutUint16(toPorts.To)},
		&expr.NAT{
			Type:        expr.NATTypeDestNAT,
			Family:      unix.NFPROTO_IPV4,
			RegAddrMin:  1,
			RegAddrMax:  1,
			RegProtoMin: 2,
			RegProtoMax: 3,
		},
	)
	return out
}

// masqExprs builds the hairpin's other half: rewrite the client's source address
// to this router's, so the device replies through us (docs/firewall.md §4).
//
// It matches on the translated destination rather than on the original one,
// because by postrouting the DNAT has already happened — the packet on the wire
// is addressed to the device. Matching narrowly on (source network, device,
// port) is what keeps this from masquerading anything else crossing the same
// segment.
func masqExprs(r MasqRule) []expr.Any {
	prefix := r.Prefix.Masked()
	bits := prefix.Bits()
	addr := prefix.Addr().AsSlice()
	mask := cidrMask(bits, len(addr)*8)

	out := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.NFPROTO_IPV4}},

		// ip saddr <prefix>
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: mask, Xor: make([]byte, 4)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addr},

		// ip daddr <device>
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: r.To.AsSlice()},

		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{l4proto(r.Protocol)}},
	}

	out = append(out, dportExprs(r.Ports)...)
	return append(out, &expr.Masq{})
}

// dportExprs matches the transport destination port, or a range of them.
//
// The offset is 2 into the transport header, which is where the destination port
// sits for both TCP and UDP — the one place the two protocols agree closely
// enough to share an expression.
func dportExprs(ports PortRange) []expr.Any {
	load := &expr.Payload{
		DestRegister: 1,
		Base:         expr.PayloadBaseTransportHeader,
		Offset:       2,
		Len:          2,
	}
	if ports.Single() {
		return []expr.Any{load, &expr.Cmp{
			Op: expr.CmpOpEq, Register: 1,
			Data: binaryutil.BigEndian.PutUint16(ports.From),
		}}
	}
	// A range expression rather than two comparisons: it is what nft itself
	// emits, so the rule reads back as `dport 30000-30010` rather than as a pair
	// of inequalities somebody has to recombine in their head.
	return []expr.Any{load, &expr.Range{
		Op:       expr.CmpOpEq,
		Register: 1,
		FromData: binaryutil.BigEndian.PutUint16(ports.From),
		ToData:   binaryutil.BigEndian.PutUint16(ports.To),
	}}
}

func l4proto(p Protocol) byte {
	if p == ProtocolUDP {
		return unix.IPPROTO_UDP
	}
	return unix.IPPROTO_TCP
}

// cidrMask builds a big-endian prefix mask, the way the wire holds it.
//
// Written out rather than reached for from net, because the one trap in this
// file is that addresses are compared against *wire* bytes while marks and
// verdicts are native-endian — and a mask built by the wrong helper fails
// silently by matching the wrong half of a subnet.
func cidrMask(ones, bits int) []byte {
	out := make([]byte, bits/8)
	for i := 0; i < ones; i++ {
		out[i/8] |= 1 << (7 - uint(i%8))
	}
	return out
}

// nulTerminated is how the kernel compares an interface name: a fixed 16-byte
// buffer, NUL-padded. Comparing the bare string matches nothing.
func nulTerminated(s string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, s)
	return b
}
