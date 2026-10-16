package gateway

import (
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/spf13/cobra"
)

// DestinationAssignment selects an exit by destination IP or CIDR.
type DestinationAssignment struct {
	Prefix string `json:"prefix"`
	Exit   string `json:"exit"`
}

func destinationPrefix(s string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(s); err == nil && a.Zone() == "" && !a.Is4In6() {
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || p.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("expected an IP address or CIDR prefix")
	}
	return p.Masked(), nil
}

func validateDestinations(r *Result, c Config) {
	seen := map[netip.Prefix]bool{}
	for i, a := range c.Destinations {
		path := fmt.Sprintf("destinations[%d]", i)
		p, err := destinationPrefix(a.Prefix)
		if err != nil {
			r.errorf(path+".prefix", "%v", err)
		} else {
			if seen[p] {
				r.errorf(path+".prefix", "duplicate destination %s", p)
			}
			seen[p] = true
		}
		if _, ok := c.Find(a.Exit); !ok || a.Exit == "" {
			r.errorf(path+".exit", "unknown exit %q", a.Exit)
		}
	}
}

// DestinationRule is restricted to an adopted LAN source and interface.
type DestinationRule struct {
	SourceRule
	Destination netip.Prefix
}

func (r DestinationRule) Line() string {
	return fmt.Sprintf("nft destination %s on %s source %s mark %#08x via %s unless dnat", r.Destination, r.Interface, r.Prefix, r.Mark, r.Exit)
}

func renderDestinations(d *Desired, c Config, links LinkView) {
	infos, err := links.Interfaces()
	if err != nil {
		return
	}
	for _, a := range c.Destinations {
		p, err := destinationPrefix(a.Prefix)
		e, ok := c.Find(a.Exit)
		if err != nil || !ok {
			continue
		}
		for _, info := range infos {
			if !info.Adopted {
				continue
			}
			for _, src := range info.Prefixes {
				if !src.IsValid() || src.Addr().Is6() != p.Addr().Is6() {
					continue
				}
				d.Table.Destinations = append(d.Table.Destinations, DestinationRule{
					SourceRule: SourceRule{Interface: info.Name, Prefix: src.Masked(), Exit: e.Name, Mark: e.Mark()}, Destination: p})
			}
		}
	}
	sort.SliceStable(d.Table.Destinations, func(i, j int) bool {
		a, b := d.Table.Destinations[i], d.Table.Destinations[j]
		if a.Destination.Bits() != b.Destination.Bits() {
			return a.Destination.Bits() > b.Destination.Bits()
		}
		return a.Line() < b.Line()
	})
}

func destinationCommand(remove bool) *cobra.Command {
	use, short, n := "destination <ip-or-cidr> <exit>", "Route a destination through an exit (overrides device and network choices)", 2
	if remove {
		use, short, n = "destination <ip-or-cidr>", "Remove a destination routing rule", 1
	}
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(n), ValidArgsFunction: cli.CompleteArgs(destinationNames, exitNames), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := destinationPrefix(args[0])
		if err != nil {
			return err
		}
		method := http.MethodPut
		body := assignmentBody{}
		if remove {
			method = http.MethodDelete
		} else {
			body.Exit = args[1]
		}
		return send(cmd, method, strings.TrimSuffix(configEndpoint, "/config")+"/destinations/"+p.Addr().String()+"/"+fmt.Sprint(p.Bits()), body)
	}}
}

func destinationNames(cmd *cobra.Command) ([]string, error) {
	c, err := loadConfig(cmd)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range c.Destinations {
		out = append(out, a.Prefix)
	}
	return out, nil
}

func (h HTTP) putDestination(w http.ResponseWriter, r *http.Request) {
	var body assignmentBody
	if err := core.DecodeJSON(w, r, &body); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.editDestination(w, r, body.Exit, false)
}

func (h HTTP) deleteDestination(w http.ResponseWriter, r *http.Request) {
	h.editDestination(w, r, "", true)
}

func (h HTTP) editDestination(w http.ResponseWriter, r *http.Request, exit string, remove bool) {
	p, err := destinationPrefix(r.PathValue("address") + "/" + r.PathValue("bits"))
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mutate(w, r, func(c *Config) error {
		out := make([]DestinationAssignment, 0, len(c.Destinations)+1)
		for _, a := range c.Destinations {
			old, _ := destinationPrefix(a.Prefix)
			if old != p {
				out = append(out, a)
			}
		}
		if !remove {
			out = append(out, DestinationAssignment{Prefix: p.String(), Exit: exit})
		}
		c.Destinations = out
		return nil
	})
}
