package host

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
	"os"
)

func (a Applier) readRecovery(name, path string) ([]byte, error) {
	if a.RuntimeError != nil {
		return nil, a.RuntimeError
	}
	if a.Runtime == nil {
		return os.ReadFile(path)
	}
	if err := a.Runtime.ImportRecovery(name, path); err != nil {
		return nil, err
	}
	return a.Runtime.Recovery(name)
}
func (a Applier) saveRecovery(name, path string, data []byte) error {
	if a.RuntimeError != nil {
		return a.RuntimeError
	}
	if a.Runtime != nil {
		return a.Runtime.SaveRecovery(name, data)
	}
	return core.WriteFileAtomic(path, data, 0600)
}
func (a Applier) deleteRecovery(name, path string) error {
	if a.RuntimeError != nil {
		return a.RuntimeError
	}
	if a.Runtime != nil {
		return a.Runtime.DeleteRecovery(name)
	}
	return os.Remove(path)
}
