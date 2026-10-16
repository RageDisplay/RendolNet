// Package agent — сторона узла: применяет конфиг к ядру (netlink, eBPF),
// держит таймер подтверждения и отдаёт состояние rendol-api через unix-сокет.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"rendolnet/internal/netcfg"
)

// Applied — конфиг вместе с номером ревизии, который выдал rendol-api.
type Applied struct {
	Revision int64         `json:"revision"`
	Config   netcfg.Config `json:"config"`
	Applied  time.Time     `json:"applied"`
	Deadline *time.Time    `json:"deadline,omitempty"`
}

// store хранит на диске подтверждённый конфиг. Агент поднимается с ним после
// перезагрузки, даже если rendol-api и база ещё не запустились.
type store struct {
	dir string
}

func (s store) path(name string) string { return filepath.Join(s.dir, name) }

func (s store) load(name string) (*Applied, error) {
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Applied
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("agent: %s повреждён: %w", name, err)
	}
	return &a, nil
}

func (s store) save(name string, a *Applied) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(name + ".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

func (s store) remove(name string) {
	_ = os.Remove(s.path(name))
}
