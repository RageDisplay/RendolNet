package update

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Обмен между rendol-api (в поде) и rendol-updater (на узле) идёт через
// файлы в общем каталоге. Так у пода нет никаких прав на хост, кроме записи
// в один каталог.
const (
	DefaultRoot = "/var/lib/rendolnet/host"
	StagingDir  = "staging"
	RequestFile = "request.json"
	StatusFile  = "status.json"
	VersionFile = "version"
)

type Action string

const (
	ActionUpdate   Action = "update"
	ActionRollback Action = "rollback"
	ActionReboot   Action = "reboot"
	ActionPoweroff Action = "poweroff"
)

type Request struct {
	ID      string    `json:"id"`
	Action  Action    `json:"action"`
	Version string    `json:"version,omitempty"`
	User    string    `json:"user"`
	Created time.Time `json:"created"`
}

type State string

const (
	StateIdle       State = "idle"
	StateQueued     State = "queued"
	StateRunning    State = "running"
	StateDone       State = "done"
	StateFailed     State = "failed"
	StateRolledBack State = "rolled_back"
)

type Status struct {
	RequestID string     `json:"request_id,omitempty"`
	Action    Action     `json:"action,omitempty"`
	State     State      `json:"state"`
	Version   string     `json:"version,omitempty"`
	From      string     `json:"from,omitempty"`
	Step      string     `json:"step,omitempty"`
	Message   string     `json:"message,omitempty"`
	Started   *time.Time `json:"started,omitempty"`
	Finished  *time.Time `json:"finished,omitempty"`
	Log       []string   `json:"log,omitempty"`
}

func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ReadStatus(root string) Status {
	var s Status
	b, err := os.ReadFile(filepath.Join(root, StatusFile))
	if errors.Is(err, fs.ErrNotExist) || err != nil || json.Unmarshal(b, &s) != nil {
		return Status{State: StateIdle}
	}
	return s
}

func ReadRequest(root string) (*Request, error) {
	b, err := os.ReadFile(filepath.Join(root, RequestFile))
	if err != nil {
		return nil, err
	}
	var r Request
	return &r, json.Unmarshal(b, &r)
}
