package link

import (
	"net/netip"
	"testing"
)

func TestIPv6AutoValidationPlanAndRelease(t *testing.T) {
	c := Config{Adopted: []string{"eth0"}, IPv6Auto: map[string]bool{"eth0": false}}
	if !Validate(c, nil).OK() {
		t.Fatal("valid setting rejected")
	}
	plan := buildPlan(Config{Adopted: c.Adopted}, c, nil, Options{}, netip.Prefix{})
	if plan.Empty || plan.Impact != impactDisruptive {
		t.Fatalf("plan = %+v", plan)
	}
	clone := c.Clone()
	clone.IPv6Auto["eth0"] = true
	if c.IPv6Auto["eth0"] {
		t.Fatal("clone shares interface settings")
	}
	if Validate(Config{IPv6Auto: c.IPv6Auto}, nil).OK() {
		t.Fatal("unadopted interface accepted")
	}
	c.Release("eth0")
	if len(c.IPv6Auto) != 0 {
		t.Fatal("release kept IPv6 ownership")
	}
}
