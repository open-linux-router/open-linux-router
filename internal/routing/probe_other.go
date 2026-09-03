//go:build !linux

package routing

import "context"

// DialThrough cannot mark a socket off Linux, so it refuses rather than
// connecting without one.
//
// Dialling unmarked would take the box's normal path and report the exit
// healthy whenever the *box* has internet — which is the single most misleading
// thing this function could do, and precisely the failure §5.5 rejects ICMP
// for. A probe that says "I could not check" is honest; one that checks the
// wrong path is worse than none.
func DialThrough(context.Context, uint32, string) error { return ErrUnsupported }
