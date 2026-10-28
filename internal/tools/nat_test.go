package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const classicTable = `Testing...
╭────────────┬────────────────────╮
│ Property   │ Value              │
├────────────┼────────────────────┤
│ NAT type   │ PortRestrictedCone │
│ Public end │ 222.128.1.95:54394 │
│ Local end  │ 198.18.0.1:55072   │
╰────────────┴────────────────────╯
`

const behaviorTable = `Testing...
╭────────────────────┬─────────────────────────╮
│ Property           │ Value                   │
├────────────────────┼─────────────────────────┤
│ Binding test       │ Success                 │
│ Mapping behavior   │ EndpointIndependent     │
│ Filtering behavior │ AddressAndPortDependent │
│ Public end         │ 222.128.1.95:58545      │
│ Local end          │ 198.18.0.1:56078        │
╰────────────────────┴─────────────────────────╯
`

func TestNATRoute(t *testing.T) {
	h := &HTTP{RunNAT: func(context.Context) (NATResult, error) {
		return NATResult{Server: "test", Type: "full-cone", Mapped: "203.0.113.1:1234", Mapping: "endpoint-independent", Filtering: "address-dependent"}, nil
	}}
	w := httptest.NewRecorder()
	core.RouteTable(h.Routes()).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/nat", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"mapped":"203.0.113.1:1234"`) || !strings.Contains(body, `"nat_type":"full-cone"`) {
		t.Fatalf("%d: %s", w.Code, body)
	}
}

func TestParseNatTypeTester(t *testing.T) {
	rows, err := parseNatTypeTester(classicTable)
	if err != nil {
		t.Fatal(err)
	}
	if rows["NAT type"] != "PortRestrictedCone" || rows["Public end"] != "222.128.1.95:54394" || rows["Local end"] != "198.18.0.1:55072" {
		t.Fatalf("rows = %#v", rows)
	}
	if _, err := parseNatTypeTester("Testing...\nError: no such host\n"); err == nil {
		t.Fatal("accepted output with no table")
	}
}

func TestRunNATServer(t *testing.T) {
	binary := fakeNatTypeTester(t)

	result, err := runNATServer(context.Background(), binary, "stun.test:3478")
	if err != nil {
		t.Fatal(err)
	}
	if result.Server != "stun.test:3478" || result.Type != "port-restricted-cone" ||
		result.Mapped != "222.128.1.95:54394" || result.Local != "198.18.0.1:55072" ||
		result.Binding != "success" || result.Mapping != "endpoint-independent" ||
		result.Filtering != "address-and-port-dependent" || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunNATServerKeepsPartialResult(t *testing.T) {
	binary := fakeNatTypeTester(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(binary), "partial"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runNATServer(context.Background(), binary, "stun.test:3478")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "port-restricted-cone" || result.Mapping != natNotTested ||
		!strings.Contains(result.Error, "rfc5780") {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunNATFallsBackToSecondServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	binary := fakeNatTypeTester(t)
	old := natTypeTesterServers
	natTypeTesterServers = []string{"stun.bad:3478", "stun.good:3478"}
	t.Cleanup(func() { natTypeTesterServers = old })
	t.Setenv("PATH", filepath.Dir(binary))

	result, err := runNAT(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Server != "stun.good:3478" || result.Type != "port-restricted-cone" {
		t.Fatalf("result = %+v", result)
	}
}

func TestNatAnswered(t *testing.T) {
	cases := []struct {
		name   string
		result NATResult
		want   bool
	}{
		{"no endpoint", NATResult{Type: "udp-blocked"}, false},
		{"blocked with endpoint", NATResult{Type: "udp-blocked", Mapped: "203.0.113.1:1", Mapping: "failed"}, false},
		{"classified", NATResult{Type: "full-cone", Mapped: "203.0.113.1:1"}, true},
		{"behavior only", NATResult{Type: "unknown", Mapped: "203.0.113.1:1", Mapping: "endpoint-independent"}, true},
		{"single-address server", NATResult{Type: "unsupported-server", Mapped: "203.0.113.1:1", Mapping: "unsupported-server"}, false},
	}
	for _, c := range cases {
		if got := natAnswered(c.result); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRunNATReportsMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := runNAT(context.Background()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunNATServerReportsBothFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "natty")
	script := "#!/bin/sh\n/bin/echo 'Error: unreachable'\nexit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := runNATServer(context.Background(), binary, "stun.test:3478")
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunNATServerStopsOnCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "natty")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n/bin/sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := runNATServer(ctx, binary, "stun.test:3478"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

// fakeNatTypeTester writes a natty that prints the two tables, unless the
// directory holds a "partial" marker, in which case rfc5780 fails.
func fakeNatTypeTester(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	classic := filepath.Join(dir, "classic.txt")
	behavior := filepath.Join(dir, "behavior.txt")
	if err := os.WriteFile(classic, []byte(classicTable), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(behavior, []byte(behaviorTable), 0644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$2" = "stun.bad:3478" ]; then
  /bin/echo "Error: unreachable"
  exit 1
fi
case "$3" in
  rfc3489) /bin/cat "` + classic + `" ;;
  rfc5780)
    if [ -f "` + filepath.Join(dir, "partial") + `" ]; then
      /bin/echo "Error: unsupported"
      exit 1
    fi
    /bin/cat "` + behavior + `" ;;
  *) exit 2 ;;
esac
`
	binary := filepath.Join(dir, "natty")
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return binary
}
