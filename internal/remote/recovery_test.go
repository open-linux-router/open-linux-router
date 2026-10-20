package remote

import (
	"context"
	"errors"
	"testing"
)

type recoveryKernel struct {
	StaticKernel
	writes []Desired
}

func (k *recoveryKernel) Apply(ctx context.Context, d Desired) ([]Step, error) {
	k.writes = append(k.writes, d)
	return k.StaticKernel.Apply(ctx, d)
}

func TestRecoverRemovesTrialWireGuardName(t *testing.T) {
	a, _ := testApplier(t)
	k := &recoveryKernel{}
	a.Kernel = k
	previous := Config{WireGuard: WireGuard{Enabled: true, Interface: "trial0"}}
	if err := a.Recover(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	if len(k.writes) == 0 || k.writes[0].Interface != "trial0" || k.writes[0].Enabled {
		t.Fatal("trial interface was not retired", k.writes)
	}
}

func TestRecoverLeavesUnusedWireGuardAlone(t *testing.T) {
	a, k := testApplier(t)
	k.ObserveErr = errors.New("should not observe unowned WireGuard")
	if err := a.Recover(context.Background(), Config{}); err != nil {
		t.Fatal(err)
	}
}
