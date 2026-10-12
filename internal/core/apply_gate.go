package core

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

var ErrApplyHeld = errors.New("this change may interrupt connectivity; repeat the request with confirm=true after reviewing the plan")

type ApplyGate struct{ DryRun, Confirm bool }

func ParseApplyGate(r *http.Request) (ApplyGate, error) {
	var gate ApplyGate
	for name, field := range map[string]*bool{"dry_run": &gate.DryRun, "confirm": &gate.Confirm} {
		if values, ok := r.URL.Query()[name]; ok {
			if len(values) != 1 {
				return gate, fmt.Errorf("%s must occur once", name)
			}
			if values[0] == "" {
				*field = true
				continue
			}
			value, err := strconv.ParseBool(values[0])
			if err != nil {
				return gate, fmt.Errorf("%s must be true or false", name)
			}
			*field = value
		}
	}
	return gate, nil
}

// Check is called with the actual execution plan, before intent or runtime
// changes. Repairing previously stored intent needs no new confirmation.
func (g ApplyGate) Check(disruptive, repair bool) error {
	if g.DryRun || (disruptive && !g.Confirm && !repair) {
		return ErrApplyHeld
	}
	return nil
}

func ApplyQuery() []QueryParam {
	return []QueryParam{
		{Name: "dry_run", Type: "boolean", Summary: "Return the execution plan without writing intent or changing the system."},
		{Name: "confirm", Type: "boolean", Summary: "Explicitly allow a disruptive execution plan."},
	}
}
