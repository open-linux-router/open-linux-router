package devices

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const (
	nasMAC   = "aa:bb:cc:00:00:01"
	phoneMAC = "aa:bb:cc:00:00:02"
)

func groupedConfig() Config {
	return Config{
		Groups: []Group{{Name: "Serving"}, {Name: "Personal"}},
		Devices: []Device{
			{MAC: nasMAC, Name: "nas", Group: "Serving"},
			{MAC: phoneMAC, Group: "Personal"},
		},
	}
}

func TestGroupsRoundTrip(t *testing.T) {
	in := groupedConfig()
	data, err := MarshalConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by name on the way in.
	if len(out.Groups) != 2 || out.Groups[0].Name != "Personal" || out.Groups[1].Name != "Serving" {
		t.Fatalf("groups = %+v", out.Groups)
	}
	if d, _ := out.Find(nasMAC); d.Group != "Serving" {
		t.Fatalf("nas group = %q", d.Group)
	}
	if res := Validate(out); !res.OK() {
		t.Fatal(res.Err())
	}
}

func TestRenameGroupCarriesItsMembers(t *testing.T) {
	c := groupedConfig()
	if !c.RenameGroup("Serving", "HomeLab") {
		t.Fatal("rename reported no such group")
	}
	if _, ok := c.FindGroup("Serving"); ok {
		t.Fatal("old name still present")
	}
	if d, _ := c.Find(nasMAC); d.Group != "HomeLab" {
		t.Fatalf("member was left behind: group = %q", d.Group)
	}
	if d, _ := c.Find(phoneMAC); d.Group != "Personal" {
		t.Fatalf("a member of another group moved: %q", d.Group)
	}
	if res := Validate(c); !res.OK() {
		t.Fatal(res.Err())
	}
	if c.RenameGroup("nope", "x") {
		t.Fatal("renaming a missing group reported success")
	}
}

func TestRemoveGroupReleasesItsMembers(t *testing.T) {
	c := groupedConfig()
	if !c.RemoveGroup("Serving") {
		t.Fatal("remove reported no such group")
	}
	d, ok := c.Find(nasMAC)
	if !ok {
		t.Fatal("removing a group forgot the device itself")
	}
	if d.Group != "" || d.Name != "nas" {
		t.Fatalf("device = %+v, want ungrouped and still named", d)
	}
	if res := Validate(c); !res.OK() {
		t.Fatal(res.Err())
	}
}

func TestSetDeviceGroupStoresAnUndescribedDevice(t *testing.T) {
	c := Config{Groups: []Group{{Name: "IoT"}}}
	c.SetDeviceGroup("AA-BB-CC-00-00-09", "IoT")
	d, ok := c.Find("aa:bb:cc:00:00:09")
	if !ok || d.Group != "IoT" {
		t.Fatalf("device = %+v, found %v", d, ok)
	}

	c.SetDeviceGroup("aa:bb:cc:00:00:09", "")
	if d, _ := c.Find("aa:bb:cc:00:00:09"); d.Group != "" {
		t.Fatalf("still grouped: %q", d.Group)
	}
}

func TestValidateGroups(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		path string
	}{
		{
			name: "device names a group that does not exist",
			cfg:  Config{Devices: []Device{{MAC: nasMAC, Group: "Serving"}}},
			path: "devices[0].group",
		},
		{
			name: "empty group name",
			cfg:  Config{Groups: []Group{{Name: " "}}},
			path: "groups[0].name",
		},
		{
			name: "names differing only by case",
			cfg:  Config{Groups: []Group{{Name: "IoT"}, {Name: "iot"}}},
			path: "groups[1].name",
		},
		{
			name: "name too long",
			cfg:  Config{Groups: []Group{{Name: strings.Repeat("x", MaxGroupNameLen+1)}}},
			path: "groups[0].name",
		},
		{
			name: "control character",
			cfg:  Config{Groups: []Group{{Name: "a\tb"}}},
			path: "groups[0].name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Normalize()
			res := Validate(tc.cfg)
			if res.OK() {
				t.Fatal("accepted")
			}
			if res.Errors[0].Path != tc.path {
				t.Fatalf("path = %q, want %q (%v)", res.Errors[0].Path, tc.path, res.Errors)
			}
		})
	}
}

func TestPlanListsGroupChanges(t *testing.T) {
	stored := groupedConfig()
	desired := stored.Clone()
	desired.RenameGroup("Serving", "HomeLab")

	plan := buildPlan(stored, desired)
	var kinds []string
	for _, c := range plan.Changes {
		kinds = append(kinds, c.Kind+" "+c.Path)
	}
	got := strings.Join(kinds, "; ")
	for _, want := range []string{
		"update devices[" + nasMAC + "]",
		"create groups[HomeLab]",
		"delete groups[Serving]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plan %q is missing %q", got, want)
		}
	}
}

// --- HTTP ------------------------------------------------------------------

func groupsHandler(t *testing.T, seed Config) (http.Handler, Applier) {
	t.Helper()
	a := Applier{Store: core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)}
	if _, err := a.Save(seed); err != nil {
		t.Fatal(err)
	}
	return HTTP{Applier: a, Lock: core.NewLock(), Events: core.NewEvents()}.Handler(), a
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func stored(t *testing.T, a Applier) Config {
	t.Helper()
	c, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHTTPCreatesAGroup(t *testing.T) {
	h, a := groupsHandler(t, Config{})
	w := send(t, h, "PUT", "/groups/IoT", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if _, ok := stored(t, a).FindGroup("IoT"); !ok {
		t.Fatal("group not stored")
	}

	// Again is a no-op, not a conflict.
	w = send(t, h, "PUT", "/groups/IoT", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("second create: status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		Plan planView `json:"plan"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Plan.Empty {
		t.Fatalf("second create changed something: %+v", resp.Plan.Changes)
	}
}

func TestHTTPRenamesAGroupWithItsMembers(t *testing.T) {
	h, a := groupsHandler(t, groupedConfig())
	w := send(t, h, "PUT", "/groups/Serving", `{"name":"HomeLab"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if d, _ := stored(t, a).Find(nasMAC); d.Group != "HomeLab" {
		t.Fatalf("member group = %q", d.Group)
	}
}

func TestHTTPRenameRefusals(t *testing.T) {
	h, _ := groupsHandler(t, groupedConfig())
	if w := send(t, h, "PUT", "/groups/Nope", `{"name":"X"}`); w.Code != http.StatusNotFound {
		t.Errorf("missing group: status %d", w.Code)
	}
	if w := send(t, h, "PUT", "/groups/Serving", `{"name":"Personal"}`); w.Code != http.StatusBadRequest {
		t.Errorf("rename onto an existing group: status %d", w.Code)
	}
	// A case-only change is a rename of the group onto itself, not a clash.
	if w := send(t, h, "PUT", "/groups/Serving", `{"name":"serving"}`); w.Code != http.StatusOK {
		t.Errorf("case-only rename: status %d: %s", w.Code, w.Body)
	}
}

func TestHTTPDeletesAGroupAndUngroupsItsDevices(t *testing.T) {
	h, a := groupsHandler(t, groupedConfig())
	if w := send(t, h, "DELETE", "/groups/Serving", ``); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	c := stored(t, a)
	if _, ok := c.FindGroup("Serving"); ok {
		t.Fatal("group still stored")
	}
	if d, _ := c.Find(nasMAC); d.Group != "" {
		t.Fatalf("device still in deleted group %q", d.Group)
	}
	if w := send(t, h, "DELETE", "/groups/Serving", ``); w.Code != http.StatusNotFound {
		t.Fatalf("second delete: status %d", w.Code)
	}
}

func TestHTTPMovesADeviceBetweenGroups(t *testing.T) {
	h, a := groupsHandler(t, groupedConfig())

	if w := send(t, h, "PUT", "/devices/"+nasMAC+"/group", `{"group":"Personal"}`); w.Code != http.StatusOK {
		t.Fatalf("move: status %d: %s", w.Code, w.Body)
	}
	if d, _ := stored(t, a).Find(nasMAC); d.Group != "Personal" || d.Name != "nas" {
		t.Fatalf("device = %+v", d)
	}

	if w := send(t, h, "PUT", "/devices/"+nasMAC+"/group", `{"group":""}`); w.Code != http.StatusOK {
		t.Fatalf("ungroup: status %d: %s", w.Code, w.Body)
	}
	if d, _ := stored(t, a).Find(nasMAC); d.Group != "" {
		t.Fatalf("still grouped: %q", d.Group)
	}

	w := send(t, h, "PUT", "/devices/"+nasMAC+"/group", `{"group":"Nope"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown group: status %d", w.Code)
	}
	if w := send(t, h, "PUT", "/devices/not-a-mac/group", `{"group":"Personal"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad mac: status %d", w.Code)
	}
}
