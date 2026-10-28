package inspection

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The switch is the one durable thing about inspection besides the CA: which
// device was being inspected, and the addresses its rule matched. It lives
// beside the CA rather than in the configuration backup, because restoring a
// backup on another router must not arm interception there.
const settingFile = "session.json"

// setting is the switch as it outlives the process. Flows never live here.
type setting struct {
	MAC string   `json:"mac"`
	IPs []string `json:"ips"`
}

func (s *Service) readSetting() (*setting, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, settingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st setting
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("reading %s: %w", settingFile, err)
	}
	if st.MAC == "" || len(st.IPs) == 0 {
		return nil, fmt.Errorf("reading %s: no device recorded", settingFile)
	}
	return &st, nil
}

// writeSetting replaces the switch atomically, so a crash while writing cannot
// leave half a device behind for the next start to resume.
func (s *Service) writeSetting(st *setting) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	target := filepath.Join(s.Dir, settingFile)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Service) clearSetting() error {
	err := os.Remove(filepath.Join(s.Dir, settingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
