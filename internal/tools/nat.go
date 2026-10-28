package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// NatTypeTester is the upstream .NET console program. Its two subcommands are
// the RFC 3489 and RFC 5780 state machines this tool borrows; see
// packaging/scripts/natty.sh for the pinned release and its checksums.
const (
	natTypeTesterTool     = "natty"
	natTypeTester3489     = "rfc3489"
	natTypeTester5780     = "rfc5780"
	natTypeTesterTestType = "Combining"
)

// A server is tried when the previous one produced nothing worth reporting:
// no public endpoint, or only an unclassifiable answer. Both servers below
// answered RFC 3489 and RFC 5780 when this list was written; miwifi is first
// because it stays reachable on mainland Chinese networks.
var natTypeTesterServers = []string{
	"stun.miwifi.com:3478",
	"stun.voipgate.com:3478",
}

// NATResult is NatTypeTester's answer, narrowed to the tokens this page
// displays. Strings are machine tokens, not prose: the UI localizes them.
type NATResult struct {
	Server string `json:"server"`
	// Type is the RFC 3489 verdict: open-internet, full-cone, restricted-cone,
	// port-restricted-cone, symmetric, symmetric-udp-firewall, udp-blocked,
	// unsupported-server or unknown.
	Type string `json:"nat_type"`
	// Mapped is the public endpoint the RFC 3489 probe observed.
	Mapped string `json:"mapped"`
	Local  string `json:"local,omitempty"`
	// Binding is the RFC 5780 binding test: success, failed or
	// unsupported-server.
	Binding   string `json:"binding,omitempty"`
	Mapping   string `json:"mapping"`
	Filtering string `json:"filtering"`
	// Error explains a partial answer: one upstream test did not finish while
	// the other still produced evidence.
	Error string `json:"error,omitempty"`
}

const natNotTested = "not tested"

// runNAT asks NatTypeTester for both classifications. They run in parallel
// because each one is a few round trips of its own; a server that answers
// neither is skipped in favour of the next one.
func runNAT(ctx context.Context) (NATResult, error) {
	binary := core.LookTool(natTypeTesterTool)
	if binary == "" {
		return NATResult{}, errors.New("NatTypeTester is not installed; install the olr package or NatTypeTester.Console 10.0.10 from github.com/HMBSbige/NatTypeTester/releases")
	}
	var last NATResult
	var lastErr error
	for _, server := range natTypeTesterServers {
		result, err := runNATServer(ctx, binary, server)
		if err != nil {
			lastErr = err
		} else {
			last = result
			if natAnswered(result) || ctx.Err() != nil {
				return result, nil
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	if last.Mapped != "" || last.Type != "" || last.Mapping != natNotTested {
		return last, nil
	}
	if lastErr != nil {
		return NATResult{}, lastErr
	}
	return last, nil
}

// natAnswered reports whether a server produced an observation worth stopping
// for. A server that answers but cannot classify is worth replacing with the
// next one; a server that only says UDP is blocked is not.
func natAnswered(result NATResult) bool {
	if result.Mapped == "" {
		return false
	}
	if result.Type != "udp-blocked" && result.Type != "unsupported-server" && result.Type != "unknown" {
		return true
	}
	return result.Mapping != natNotTested && result.Mapping != "unsupported-server" && result.Mapping != "failed"
}

// runNATServer runs both upstream tests against one server. A failure in one
// is kept as a partial result rather than thrown away: a server can support
// the RFC 3489 address exchange and not the RFC 5780 one, and the page can
// still report what it learned.
func runNATServer(ctx context.Context, binary, server string) (NATResult, error) {
	var classic, behavior map[string]string
	var classicErr, behaviorErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		classic, classicErr = runNatTypeTester(ctx, binary, server, natTypeTester3489)
	}()
	go func() {
		defer wg.Done()
		behavior, behaviorErr = runNatTypeTester(ctx, binary, server, natTypeTester5780)
	}()
	wg.Wait()
	if classicErr != nil && behaviorErr != nil {
		return NATResult{}, fmt.Errorf("NatTypeTester could not reach %s: %w", server, errors.Join(classicErr, behaviorErr))
	}

	result := NATResult{Server: server, Mapping: natNotTested, Filtering: natNotTested}
	if classicErr == nil {
		result.Type = natTypeToken(classic["NAT type"])
		result.Mapped = classic["Public end"]
		result.Local = classic["Local end"]
	} else {
		result.Type = "unknown"
		result.Error = classicErr.Error()
	}
	if behaviorErr == nil {
		result.Binding = natBindingToken(behavior["Binding test"])
		result.Mapping = natBehaviorToken(behavior["Mapping behavior"])
		result.Filtering = natBehaviorToken(behavior["Filtering behavior"])
		if result.Mapped == "" {
			result.Mapped = behavior["Public end"]
		}
		if result.Local == "" {
			result.Local = behavior["Local end"]
		}
	} else if result.Error == "" {
		result.Error = behaviorErr.Error()
	} else {
		result.Error += "; " + behaviorErr.Error()
	}
	return result, nil
}

// runNatTypeTester executes one subcommand and returns its table as key/value
// pairs. The table is the upstream CLI's only output format, so its rows are
// checked here rather than trusted to stay put.
func runNatTypeTester(ctx context.Context, binary, server, mode string) (map[string]string, error) {
	args := []string{"--server", server, mode}
	if mode == natTypeTester5780 {
		args = append(args, "--test-type", natTypeTesterTestType)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = time.Second
	cmd.Env = natTypeTesterEnv()
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.truncated || stderr.truncated {
		return nil, errors.New("NatTypeTester output exceeded 64 KiB")
	}
	if err != nil {
		// The CLI writes its failure, including a DNS error, to stdout after an
		// optional status line, and leaves stderr empty.
		reason := lastLine(stdout.String())
		if reason == "" {
			reason = lastLine(stderr.String())
		}
		if reason != "" {
			return nil, fmt.Errorf("NatTypeTester %s failed: %s: %w", mode, reason, err)
		}
		return nil, fmt.Errorf("NatTypeTester %s failed: %w", mode, err)
	}
	rows, err := parseNatTypeTester(stdout.String())
	if err != nil {
		return nil, fmt.Errorf("NatTypeTester %s: %w", mode, err)
	}
	for _, key := range natTypeTesterRequired(mode) {
		if _, ok := rows[key]; !ok {
			return nil, fmt.Errorf("NatTypeTester %s: output did not include %q", mode, key)
		}
	}
	return rows, nil
}

// natTypeTesterEnv pins the CLI's language and keeps .NET from aborting on a
// box without ICU: both upstream tables are parsed by their English labels,
// and invariant globalization is the documented way to run the native AOT
// binary without libicu installed.
func natTypeTesterEnv() []string {
	env := commandEnv()
	return append(env, "LANG=C", "LC_ALL=C", "DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1")
}

func natTypeTesterRequired(mode string) []string {
	if mode == natTypeTester5780 {
		return []string{"Binding test", "Mapping behavior", "Filtering behavior"}
	}
	return []string{"NAT type"}
}

// parseNatTypeTester reads the rounded table the console prints. Values are
// plain enum names and endpoints, so splitting on the column separator is
// enough; a line without it is the status line or a blank.
func parseNatTypeTester(output string) (map[string]string, error) {
	rows := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		cells := strings.Split(line, "│")
		if len(cells) < 3 {
			continue
		}
		key, value := strings.TrimSpace(cells[1]), strings.TrimSpace(cells[2])
		if key == "" || key == "Property" {
			continue
		}
		rows[key] = value
	}
	if len(rows) == 0 {
		return nil, errors.New("no result table in output")
	}
	return rows, nil
}

// The upstream enum names are stable; the tokens below are this API's own and
// match the naming the rest of the tools package uses.
func natTypeToken(value string) string {
	switch value {
	case "OpenInternet":
		return "open-internet"
	case "FullCone":
		return "full-cone"
	case "RestrictedCone":
		return "restricted-cone"
	case "PortRestrictedCone":
		return "port-restricted-cone"
	case "Symmetric":
		return "symmetric"
	case "SymmetricUdpFirewall":
		return "symmetric-udp-firewall"
	case "UdpBlocked":
		return "udp-blocked"
	case "UnsupportedServer":
		return "unsupported-server"
	case "Unknown":
		return "unknown"
	}
	return "unknown"
}

func natBehaviorToken(value string) string {
	switch value {
	case "Direct":
		return "direct"
	case "EndpointIndependent":
		return "endpoint-independent"
	case "AddressDependent":
		return "address-dependent"
	case "AddressAndPortDependent":
		return "address-and-port-dependent"
	case "UnsupportedServer":
		return "unsupported-server"
	case "Fail":
		return "failed"
	}
	return natNotTested
}

func natBindingToken(value string) string {
	switch value {
	case "Success":
		return "success"
	case "Fail":
		return "failed"
	case "UnsupportedServer":
		return "unsupported-server"
	}
	return natNotTested
}
