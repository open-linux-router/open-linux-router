package inspection

import (
	"strings"
	"sync"
	"syscall"
	"time"
)

// terminate gives the PyInstaller launcher time to reap its listener and remove
// its extraction directory. Killing both with SIGKILL leaks about 100 MiB on
// every restart. A negative target addresses the whole owned process group.
func terminate(target int) {
	if target == 0 || syscall.Kill(target, syscall.SIGTERM) != nil {
		return
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for syscall.Kill(target, 0) != syscall.ESRCH {
		select {
		case <-deadline.C:
			_ = syscall.Kill(target, syscall.SIGKILL)
			return
		case <-ticker.C:
		}
	}
}

// proxyLog retains only the latest startup diagnostics, never an unbounded
// stderr stream. exec drains stderr concurrently, including while open holds mu.
type proxyLog struct {
	mu   sync.Mutex
	tail string
}

func (l *proxyLog) Write(p []byte) (int, error) {
	const limit = 4096
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(p) >= limit {
		l.tail = string(p[len(p)-limit:])
	} else {
		l.tail += string(p)
		if len(l.tail) > limit {
			l.tail = l.tail[len(l.tail)-limit:]
		}
	}
	return len(p), nil
}

func (l *proxyLog) message(token string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	// A command-line diagnostic may repeat the arguments, including the token
	// supplied by OLR. It must never become part of an API error.
	message := l.tail
	if token != "" {
		// The ring may begin partway through a token. Redact that suffix too,
		// including when stderr delivered the token across multiple writes.
		for cut := 1; cut < len(token); cut++ {
			if strings.HasPrefix(message, token[cut:]) {
				message = "[redacted]" + message[len(token)-cut:]
				break
			}
		}
		message = strings.ReplaceAll(message, token, "[redacted]")
	}
	return strings.TrimSpace(message)
}
