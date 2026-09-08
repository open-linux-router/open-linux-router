package link

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func find(t *testing.T, list []Info, name string) Info {
	t.Helper()
	for _, info := range list {
		if info.Name == name {
			return info
		}
	}
	t.Fatalf("%q is not in %v", name, names(list))
	return Info{}
}

func names(list []Info) []string {
	out := make([]string, 0, len(list))
	for _, info := range list {
		out = append(out, info.Name)
	}
	return out
}

func TestJoinMarksAdoptedInterfaces(t *testing.T) {
	got := Join(Config{Adopted: []string{"lan0"}}, testInterfaces(t))

	if !find(t, got, "lan0").Adopted {
		t.Error("lan0 is adopted in the config but not in the join")
	}
	if find(t, got, "lan1").Adopted {
		t.Error("lan1 is not adopted but the join says it is")
	}
}

// The union of both key sets, not just the kernel's. Without this a typo in
// `olr adopt` is invisible: the name is stored, every module keeps refusing to
// use it, and the one screen that could explain why shows nothing at all.
func TestJoinKeepsAnAdoptedInterfaceThatDoesNotExist(t *testing.T) {
	got := Join(Config{Adopted: []string{"eth9"}}, testInterfaces(t))

	ghost := find(t, got, "eth9")
	switch {
	case !ghost.Adopted:
		t.Error("eth9 is adopted but the join says it is not")
	case ghost.Present:
		t.Error("eth9 does not exist but the join says it is present")
	}

	// And it sorts to the end, after the real interfaces including loopback.
	if last := got[len(got)-1].Name; last != "eth9" {
		t.Errorf("last row is %q, want the absent interface eth9; order was %v", last, names(got))
	}
}

func TestFactsInterfaceReportsAnUnknownName(t *testing.T) {
	facts := Facts{Source: staticSource(testInterfaces(t)...), Store: storeWith(t, "")}

	if _, err := facts.Interface("nope"); !errors.Is(err, ErrNoSuchInterface) {
		t.Errorf("Interface(nope) = %v, want ErrNoSuchInterface", err)
	}
}

// The join reads adoption from the document, so a store that has one reports it.
func TestFactsReadsAdoptionFromTheDocument(t *testing.T) {
	facts := Facts{
		Source: staticSource(testInterfaces(t)...),
		Store:  storeWith(t, `{"link":{"adopted":["lan0"]}}`),
	}

	info, err := facts.Interface("lan0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Adopted {
		t.Error("lan0 is adopted in the document but Facts says it is not")
	}
	if len(info.Prefixes) != 1 || info.Prefixes[0].String() != "192.168.1.2/24" {
		t.Errorf("Prefixes = %v, want the observed 192.168.1.2/24", info.Prefixes)
	}
}

// --- the development override ----------------------------------------------

func TestLoadFileReadsInterfaceFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.json")
	body := `{"lan0": {"up": true, "prefixes": ["192.168.10.1/24"]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	source, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "lan0" || !got[0].Up {
		t.Fatalf("LoadFile = %+v, want one interface lan0, up", got)
	}
	if len(got[0].Prefixes) != 1 || got[0].Prefixes[0].String() != "192.168.10.1/24" {
		t.Errorf("Prefixes = %v", got[0].Prefixes)
	}
}

// The file used to carry adoption because there was nowhere else to put it.
// There is now, and two sources for one fact is what §4.1 forbids — so the key
// is accepted for compatibility and does nothing.
func TestLoadFileIgnoresTheAdoptedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.json")
	body := `{"lan0": {"adopted": true, "up": true, "prefixes": ["192.168.10.1/24"]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	source, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile rejected a file carrying the old adopted key: %v", err)
	}
	observed, err := source()
	if err != nil {
		t.Fatal(err)
	}

	// Adoption comes from the document, which here has none.
	if find(t, Join(Config{}, observed), "lan0").Adopted {
		t.Error("the file's adopted key reached the join; it must be ignored")
	}
}
