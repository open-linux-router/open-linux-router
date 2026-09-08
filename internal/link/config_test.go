package link

import (
	"slices"
	"testing"
)

func TestNormalizeSortsTrimsAndDeduplicates(t *testing.T) {
	cfg := Config{Adopted: []string{"  eth1 ", "eth0", "eth1", "", "   "}}
	cfg.Normalize()

	want := []string{"eth0", "eth1"}
	if !slices.Equal(cfg.Adopted, want) {
		t.Errorf("Adopted = %v, want %v", cfg.Adopted, want)
	}
}

// Interface names are case-sensitive on Linux, so folding them would silently
// adopt an interface the operator did not name.
func TestNormalizeKeepsCase(t *testing.T) {
	cfg := Config{Adopted: []string{"ETH0", "eth0"}}
	cfg.Normalize()

	if len(cfg.Adopted) != 2 {
		t.Errorf("Adopted = %v, want both spellings kept as distinct interfaces", cfg.Adopted)
	}
}

// An empty list marshals as an absent key rather than `"adopted": []`. The two
// mean the same thing and only one of them is worth writing to a file.
func TestEmptyConfigMarshalsWithoutTheKey(t *testing.T) {
	data, err := MarshalConfig(Config{Adopted: []string{"  "}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}" {
		t.Errorf("MarshalConfig = %s, want {}", data)
	}
}

func TestConfigRoundTrips(t *testing.T) {
	data, err := MarshalConfig(Config{Adopted: []string{"lan1", "lan0"}})
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"lan0", "lan1"}; !slices.Equal(back.Adopted, want) {
		t.Errorf("round trip = %v, want %v", back.Adopted, want)
	}
}

// A typo'd key that is silently ignored produces a box that is quietly not
// doing what its config says.
func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	if _, err := UnmarshalConfig([]byte(`{"adopted":["eth0"],"adoped":["eth1"]}`)); err == nil {
		t.Error("UnmarshalConfig accepted an unknown field")
	}
}

func TestAdoptAndReleaseAreIdempotent(t *testing.T) {
	var cfg Config

	if !cfg.Adopt("eth0") {
		t.Fatal("first Adopt reported no change")
	}
	if cfg.Adopt("eth0") {
		t.Error("second Adopt reported a change; adopting twice is a no-op")
	}
	if !cfg.IsAdopted("eth0") {
		t.Error("IsAdopted is false after Adopt")
	}

	if !cfg.Release("eth0") {
		t.Fatal("first Release reported no change")
	}
	if cfg.Release("eth0") {
		t.Error("second Release reported a change; releasing twice is a no-op")
	}
	if !cfg.Empty() {
		t.Errorf("Empty is false after releasing everything: %v", cfg.Adopted)
	}
}

// Clone has to break the backing array, or diffing a proposal against the
// stored config would compare a slice with itself.
func TestCloneDoesNotShareBackingArray(t *testing.T) {
	cfg := Config{Adopted: []string{"eth0"}}
	clone := cfg.Clone()
	clone.Adopted[0] = "eth1"

	if cfg.Adopted[0] != "eth0" {
		t.Errorf("mutating the clone changed the original: %v", cfg.Adopted)
	}
}

// A document with no "link" key is a fresh install, not a failure.
func TestFromDocumentTreatsAnAbsentSectionAsEmpty(t *testing.T) {
	cfg, err := FromDocument(documentOf(t, `{"dhcp":{"enabled":false}}`))
	if err != nil {
		t.Fatalf("FromDocument on a document without a link section: %v", err)
	}
	if !cfg.Empty() {
		t.Errorf("Adopted = %v, want empty", cfg.Adopted)
	}
}

func TestFromDocumentReadsTheSection(t *testing.T) {
	cfg, err := FromDocument(documentOf(t, `{"link":{"adopted":["lan0"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsAdopted("lan0") {
		t.Errorf("Adopted = %v, want lan0", cfg.Adopted)
	}
}
