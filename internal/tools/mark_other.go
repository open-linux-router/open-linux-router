//go:build !linux

package tools

import (
	"errors"
	"syscall"
)

func markSocket(uint32) func(string, string, syscall.RawConn) error {
	return func(string, string, syscall.RawConn) error {
		return errors.New("selecting a way out requires Linux")
	}
}
