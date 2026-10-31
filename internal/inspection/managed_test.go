package inspection

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type inspectionTestUnit struct {
	starts, stops int
	active        bool
}

func (u *inspectionTestUnit) Status(context.Context) (core.UnitStatus, error) {
	return core.UnitStatus{Active: u.active, Installed: true, MainPID: 123}, nil
}
func (u *inspectionTestUnit) Start(context.Context) error   { u.starts++; u.active = true; return nil }
func (u *inspectionTestUnit) Stop(context.Context) error    { u.stops++; u.active = false; return nil }
func (u *inspectionTestUnit) Restart(context.Context) error { return nil }
func (u *inspectionTestUnit) Reload(context.Context) error  { return nil }
func (u *inspectionTestUnit) Enable(context.Context) error  { return nil }
func (u *inspectionTestUnit) Disable(context.Context) error { return nil }

func TestManagedCloseKeepsProxyAndRedirect(t *testing.T) {
	u := &inspectionTestUnit{active: true}
	q := &session{managed: true, detach: make(chan struct{})}
	s := &Service{Managed: true, ProxyUnit: u, session: q}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !u.active || u.stops != 0 || s.session != nil {
		t.Fatalf("redeploy stopped independent proxy: %+v", u)
	}
	select {
	case <-q.detach:
	default:
		t.Fatal("redeploy did not detach observation")
	}
}

func TestManagedResumeReattachesWithoutChangingDataPath(t *testing.T) {
	proxy, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Skip(err)
	}
	defer proxy.Close()
	viewer, err := net.Listen("tcp", "127.0.0.1:"+webPort)
	if err != nil {
		t.Skip(err)
	}
	defer viewer.Close()
	token := strings.Repeat("a", 48)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.WriteHeader(200)
	})}
	go srv.Serve(viewer)
	defer srv.Close()
	dir := t.TempDir()
	bin := t.TempDir()
	// Reinstallation or deletion would run a mutating nft command. Only the
	// read of the already-installed counter is permitted during reattachment.
	script := `#!/bin/sh
if [ "$1" != '-j' ]; then exit 77; fi
printf '%s\n' '{"nftables":[{"counter":{"family":"inet","table":"olr_inspection","name":"redirected","packets":42}}]}'
`
	if err := os.WriteFile(filepath.Join(bin, "nft"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	u := &inspectionTestUnit{active: true}
	s := &Service{Enabled: true, Managed: true, Dir: dir, ProxyUnit: u}
	st := &setting{MAC: "ae:59:ff:67:0c:2c", IPs: []string{"172.16.1.43"}}
	if err := s.writeSetting(st); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(managedState{MAC: st.MAC, IPs: st.IPs, Token: token, Ready: true})
	if err := os.WriteFile(filepath.Join(dir, "proxy.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.session == nil || !s.session.managed || u.starts != 0 || u.stops != 0 {
		t.Fatalf("did not attach existing proxy: session=%+v unit=%+v problem=%s", s.session, u, s.problem)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedUnitHasIndependentOwnershipAndCleanup(t *testing.T) {
	unit := string(managedUnit("/opt/mitm web", "/usr/sbin/nft", "/var/lib/olr%test", []string{"--set", "web_password=$secret%", "-s", "/private/addon.py"}, 0x10000))
	for _, need := range []string{"KillSignal=SIGTERM", "TimeoutStopSec=5", "ExecStopPost=-\"/usr/sbin/nft\" delete table inet olr_inspection", "StandardOutput=null", "Group=2147418113", "$$secret%%", "TMPDIR=/var/lib/olr%%test/tmp"} {
		if !strings.Contains(unit, need) {
			t.Fatalf("unit lacks %q:\n%s", need, unit)
		}
	}
	for _, forbidden := range []string{"PartOf=olrd", "BindsTo=olrd", "Requires=olrd"} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("proxy depends on daemon: %s", unit)
		}
	}
}

func TestRetentionDefaultsAndBounds(t *testing.T) {
	s := &Service{Dir: t.TempDir()}
	for _, tc := range []struct {
		body string
		want int
	}{{`{"days":30,"max_bytes":5368709120}`, 200}, {`{"days":0,"max_bytes":5368709120}`, 400}, {`{"days":30,"max_bytes":1}`, 400}} {
		r := httptest.NewRequest(http.MethodPut, "/retention", strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		s.retention(w, r)
		if w.Code != tc.want {
			t.Fatalf("policy=%s response=%d %s", tc.body, w.Code, w.Body.String())
		}
	}
	info, err := os.Stat(filepath.Join(s.Dir, "retention.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("retention permissions %v %v", info, err)
	}
}

func TestManagedResumeRemovesRedirectWhenRouteBecomesBlocked(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "removed")
	if err := os.WriteFile(filepath.Join(bin, "nft"), []byte("#!/bin/sh\nprintf deleted > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	u := &inspectionTestUnit{active: true}
	s := &Service{Enabled: true, Managed: true, Dir: t.TempDir(), ProxyUnit: u, ExitMark: func(string) (uint32, error) { return 0, fmt.Errorf("exit blocked") }}
	if err := s.writeSetting(&setting{MAC: "ae:59:ff:67:0c:2c", IPs: []string{"172.16.1.43"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u.active || u.stops != 1 || s.session != nil || !strings.Contains(s.problem, "blocked") {
		t.Fatalf("stale interception survived: unit=%+v problem=%s", u, s.problem)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("redirect was not removed: %v", err)
	}
}

func TestManagedNoSettingStopsProxyEvenWhenCleanupFails(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nft"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	u := &inspectionTestUnit{active: true}
	s := &Service{Enabled: true, Managed: true, Dir: t.TempDir(), ProxyUnit: u}
	if err := s.Resume(context.Background()); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	if u.active || u.stops != 1 {
		t.Fatal("cleanup failure prevented stopping the stale proxy")
	}
}

func TestManagedActivityFailureRemainsVisible(t *testing.T) {
	s := &Service{Dir: t.TempDir(), Activity: NewActivity(nil, nil)}
	if err := s.beginActivity(context.Background(), "ae:59:ff:67:0c:2c", []string{"172.16.1.43"}); err == nil {
		t.Fatal("expected unconfigured storage error")
	}
	s.problem = ""
	if got := s.snapshot().HistoryError; !strings.Contains(got, "storage") {
		t.Fatalf("activity restoration error disappeared: %q", got)
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureActivityStorage(file); err == nil || s.Activity != nil || s.historyProblem == "" {
		t.Fatal("optional activity storage failure was not reported")
	}
}

func TestManagedRejectsIncompleteInstallation(t *testing.T) {
	u := &inspectionTestUnit{active: true}
	s := &Service{Dir: t.TempDir(), Managed: true, ProxyUnit: u}
	st := managedState{MAC: "ae:59:ff:67:0c:2c", IPs: []string{"172.16.1.43"}, Token: strings.Repeat("a", 48)}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(s.Dir, "proxy.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	attached, err := s.attachManaged(context.Background(), st.MAC, st.IPs, 0)
	if attached || err == nil {
		t.Fatal("partial nft install was accepted as an active inspection")
	}
}

func TestManagedIdentityRejectsWrongViewerToken(t *testing.T) {
	viewer, err := net.Listen("tcp", "127.0.0.1:"+webPort)
	if err != nil {
		t.Skip(err)
	}
	defer viewer.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", 401) })}
	go srv.Serve(viewer)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := proxyAuthenticated(ctx, "wrong"); err == nil {
		t.Fatal("accepted an unrelated process on the viewer port")
	}
}
