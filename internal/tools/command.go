package tools

import (
	"os"
	"strings"
)

// commandEnv is the daemon environment without proxy variables. Those would
// send an external measurement through the proxy's route instead of the router
// default route or the way out the operator picked.
func commandEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToLower(key) {
		case "http_proxy", "https_proxy", "all_proxy":
			continue
		}
		env = append(env, entry)
	}
	return env
}
