//go:build !linux

package inspection

// killOrphans is a no-op where the proxy cannot be started in the first place.
func (s *Service) killOrphans() {}
