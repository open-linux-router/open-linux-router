package devices

import (
	"strings"
	"testing"
)

func TestUnmarshalConfigRoundTrip(t *testing.T) {
	in := `{
	  "devices": [
	    {"mac": "AA-BB-CC-DD-EE-FF", "name": "Study laptop", "category": "laptop"},
	    {"mac": "11:22:33:44:55:66", "model": "synology/ds224plus", "notes": "loft"}
	  ]
	}`

	cfg, err := UnmarshalConfig([]byte(in))
	if err != nil {
		t.Fatalf("UnmarshalConfig failed: %v", err)
	}
	if len(cfg.Devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(cfg.Devices))
	}

	// Normalize sorts by MAC, so 11:… precedes aa:….
	if cfg.Devices[0].MAC != "11:22:33:44:55:66" {
		t.Errorf("devices[0].MAC = %q; want the list sorted by MAC", cfg.Devices[0].MAC)
	}
	if cfg.Devices[1].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("devices[1].MAC = %q; want it canonicalised", cfg.Devices[1].MAC)
	}

	out, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}
	again, err := UnmarshalConfig(out)
	if err != nil {
		t.Fatalf("re-parsing our own output failed: %v", err)
	}
	if len(again.Devices) != 2 || again.Devices[1].Name != "Study laptop" {
		t.Errorf("round trip lost data: %+v", again.Devices)
	}
}

// A mistyped key that silently did nothing would be the worst outcome: a 200,
// an operator who believes the setting took, and a screen that disagrees.
func TestUnmarshalConfigRejectsUnknownFields(t *testing.T) {
	_, err := UnmarshalConfig([]byte(`{"devices": [{"mac": "aa:bb:cc:dd:ee:ff", "icon": "laptop"}]}`))
	if err == nil {
		t.Fatal("UnmarshalConfig accepted an unknown field")
	}
	if !strings.Contains(err.Error(), "icon") {
		t.Errorf("error should name the offending field, got: %v", err)
	}
}

func TestUnmarshalConfigAcceptsAnEmptyDocument(t *testing.T) {
	cfg, err := UnmarshalConfig([]byte(`{}`))
	if err != nil {
		t.Fatalf("UnmarshalConfig({}) failed: %v", err)
	}
	if !cfg.Empty() {
		t.Errorf("want an empty config, got %+v", cfg)
	}
}

func TestNormalizeTrimsAndLowercases(t *testing.T) {
	cfg := Config{Devices: []Device{{
		MAC:   "AA:BB:CC:DD:EE:FF",
		Name:  "  Study laptop  ",
		Model: "  Synology/DS224Plus  ",
		Notes: "  in the loft  ",
	}}}
	cfg.Normalize()

	d := cfg.Devices[0]
	if d.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q", d.MAC)
	}
	if d.Name != "Study laptop" {
		t.Errorf("Name = %q, want it trimmed but not case-folded", d.Name)
	}
	if d.Model != "synology/ds224plus" {
		t.Errorf("Model = %q, want it lower-cased", d.Model)
	}
	if d.Notes != "in the loft" {
		t.Errorf("Notes = %q", d.Notes)
	}
}

// Sorting on the way in is what stops a renamed row jumping to the bottom of
// the list — the append-on-edit behaviour the dhcp tables still have.
func TestUpsertKeepsThePositionStable(t *testing.T) {
	cfg := Config{}
	cfg.Upsert(Device{MAC: "11:22:33:44:55:66", Name: "one"})
	cfg.Upsert(Device{MAC: "aa:bb:cc:dd:ee:ff", Name: "two"})
	cfg.Upsert(Device{MAC: "55:66:77:88:99:aa", Name: "three"})

	before := indexOf(cfg, "11:22:33:44:55:66")
	cfg.Upsert(Device{MAC: "11:22:33:44:55:66", Name: "one renamed"})
	after := indexOf(cfg, "11:22:33:44:55:66")

	if before != after {
		t.Errorf("editing moved the row from index %d to %d", before, after)
	}
	if len(cfg.Devices) != 3 {
		t.Errorf("got %d devices, want 3: upsert should replace, not append", len(cfg.Devices))
	}
	if got, _ := cfg.Find("11:22:33:44:55:66"); got.Name != "one renamed" {
		t.Errorf("Name = %q, want the new value", got.Name)
	}
}

func TestUpsertCanonicalisesTheKey(t *testing.T) {
	cfg := Config{}
	cfg.Upsert(Device{MAC: "aa:bb:cc:dd:ee:ff", Name: "first"})
	cfg.Upsert(Device{MAC: "AA-BB-CC-DD-EE-FF", Name: "second"})

	if len(cfg.Devices) != 1 {
		t.Fatalf("got %d devices, want 1: those are the same hardware", len(cfg.Devices))
	}
	if cfg.Devices[0].Name != "second" {
		t.Errorf("Name = %q, want %q", cfg.Devices[0].Name, "second")
	}
}

func TestFindAndRemove(t *testing.T) {
	cfg := Config{Devices: []Device{{MAC: "aa:bb:cc:dd:ee:ff", Name: "laptop"}}}

	if _, ok := cfg.Find("AA-BB-CC-DD-EE-FF"); !ok {
		t.Error("Find should canonicalise its argument")
	}
	if _, ok := cfg.Find("11:22:33:44:55:66"); ok {
		t.Error("Find returned a device that is not there")
	}
	if !cfg.Remove("AA:BB:CC:DD:EE:FF") {
		t.Error("Remove should canonicalise its argument")
	}
	if !cfg.Empty() {
		t.Errorf("want an empty config after removing the only device, got %+v", cfg)
	}
	if cfg.Remove("aa:bb:cc:dd:ee:ff") {
		t.Error("Remove reported success for a device that was already gone")
	}
}

func indexOf(c Config, mac string) int {
	for i, d := range c.Devices {
		if d.MAC == mac {
			return i
		}
	}
	return -1
}
