package link

import (
	"strings"
	"testing"
)

func hasProblem(in []Problem, substr string) bool {
	for _, p := range in {
		if strings.Contains(p.Message, substr) {
			return true
		}
	}
	return false
}

func TestValidateRejectsImpossibleNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"", "cannot be empty"},
		{"this-name-is-far-too-long", "longer than"},
		{"..", "not an interface name"},
		{"eth0/../etc", "slash"},
		{"eth 0", "whitespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := Validate(Config{Adopted: []string{tc.name}}, nil)
			if res.OK() {
				t.Fatalf("Validate accepted %q", tc.name)
			}
			if !hasProblem(res.Errors, tc.want) {
				t.Errorf("errors = %v, want one mentioning %q", res.Errors, tc.want)
			}
		})
	}
}

// Serving anything on loopback is nonsense, and unlike the other findings this
// one cannot become correct later, so it is an error rather than a warning.
func TestValidateRefusesLoopback(t *testing.T) {
	res := Validate(Config{Adopted: []string{"lo"}}, testInterfaces(t))

	if res.OK() {
		t.Fatal("Validate accepted the loopback interface")
	}
	if !hasProblem(res.Errors, "loopback") {
		t.Errorf("errors = %v, want one mentioning loopback", res.Errors)
	}
}

// A VLAN or a USB adapter can legitimately be adopted before it exists, so
// refusing would mean the config could not be written until the hardware was
// plugged in.
func TestValidateWarnsButAcceptsAnAbsentInterface(t *testing.T) {
	res := Validate(Config{Adopted: []string{"eth9"}}, testInterfaces(t))

	if !res.OK() {
		t.Fatalf("Validate refused an absent interface: %v", res.Errors)
	}
	if !hasProblem(res.Warnings, "no such interface") {
		t.Errorf("warnings = %v, want one saying the interface is absent", res.Warnings)
	}
}

// The one that actually bites. An interface with no address has no subnet for a
// range to fall inside, so dhcp refuses every range on it with a message about
// subnets rather than about the missing address.
func TestValidateWarnsAboutAnInterfaceWithNoAddress(t *testing.T) {
	res := Validate(Config{Adopted: []string{"lan1"}}, testInterfaces(t))

	if !res.OK() {
		t.Fatalf("Validate refused an interface with no address: %v", res.Errors)
	}
	if !hasProblem(res.Warnings, "no address configured") &&
		!hasProblem(res.Warnings, "is down") {
		t.Errorf("warnings = %v, want one about the missing address or the down state", res.Warnings)
	}
}

// Without observed facts only the name rules run, which is what keeps the rule
// set testable without a network (design.md §5.3.1).
func TestValidateWithoutObservedFactsChecksOnlyNames(t *testing.T) {
	res := Validate(Config{Adopted: []string{"eth9"}}, nil)

	if !res.OK() {
		t.Errorf("Validate refused: %v", res.Errors)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none without facts to check against", res.Warnings)
	}
}

func TestValidateAcceptsAnOrdinaryInterface(t *testing.T) {
	res := Validate(Config{Adopted: []string{"lan0"}}, testInterfaces(t))

	if !res.OK() {
		t.Errorf("Validate refused lan0: %v", res.Errors)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none for an up interface with an address", res.Warnings)
	}
}
