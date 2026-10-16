package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrNotLive = errors.New("установщик работает только с установочного носителя RendolNet")

// GuardLive не пускает дальше, если это не live-система с нашего ISO.
// Запущенный случайно на рабочем узле или ноутбуке, установщик не тронет диски.
func GuardLive() error {
	if _, err := os.Stat(LiveMarker); err != nil {
		return ErrNotLive
	}
	mounts, err := os.ReadFile("/proc/self/mounts")
	if err != nil || !strings.Contains(string(mounts), " "+LiveMediumMount+" ") {
		return ErrNotLive
	}
	if os.Geteuid() != 0 {
		return errors.New("установщик запускается от root")
	}
	return nil
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Exec struct{ Timeout time.Duration }

func (x Exec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if x.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, x.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func Inventory(ctx context.Context, r Runner) ([]Disk, error) {
	out, err := r.Run(ctx, "lsblk", LsblkArgs...)
	if err != nil {
		return nil, err
	}
	links := map[string]string{}
	entries, _ := os.ReadDir("/dev/disk/by-id")
	for _, e := range entries {
		if t, err := os.Readlink(filepath.Join("/dev/disk/by-id", e.Name())); err == nil {
			links[e.Name()] = t
		}
	}
	return ParseDisks(out, ByIDLinks(links))
}

type Progress func(i, total int, title string)

func Execute(ctx context.Context, r Runner, steps []Step, progress Progress, logf func(string, ...any)) error {
	for i, s := range steps {
		if progress != nil {
			progress(i, len(steps), s.Title)
		}
		var err error
		switch s.Kind {
		case Cmd:
			logf("$ %s", strings.Join(s.Argv, " "))
			_, err = r.Run(ctx, s.Argv[0], s.Argv[1:]...)
		case File:
			logf("write %s", s.Path)
			err = writeFile(s.Path, s.Content, os.FileMode(s.Mode))
		case Fstab:
			err = writeFstab(ctx, r, s)
		}
		if err != nil {
			logf("ошибка: %v", err)
			if s.Optional {
				continue
			}
			return fmt.Errorf("%s: %w", s.Title, err)
		}
	}
	return nil
}

func writeFile(p, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), mode)
}

func writeFstab(ctx context.Context, r Runner, s Step) error {
	uuid := func(dev string) (string, error) {
		out, err := r.Run(ctx, "blkid", "-s", "UUID", "-o", "value", dev)
		u := strings.TrimSpace(string(out))
		if err == nil && u == "" {
			err = fmt.Errorf("нет UUID у %s", dev)
		}
		return u, err
	}
	root, err := uuid(s.Argv[0])
	if err != nil {
		return err
	}
	esp, err := uuid(s.Argv[1])
	if err != nil {
		return err
	}
	return writeFile(s.Path, FstabText(root, esp), 0o644)
}

const AutoinstallLabel = "RNAUTO"

// Без rn.autoinstall=1 в строке ядра файл автоустановки игнорируется:
// забытая флешка не должна сама стереть сервер.
func AutoinstallRequested(cmdline string) bool {
	for _, f := range strings.Fields(cmdline) {
		if f == "rn.autoinstall=1" {
			return true
		}
	}
	return false
}

func ParseAutoinstall(raw []byte) (Answers, error) {
	var a Answers
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil {
		return a, fmt.Errorf("autoinstall.yaml: %w", err)
	}
	if strings.TrimSpace(a.Disk) == "" {
		return a, errors.New("autoinstall.yaml: нужен disk (путь by-id)")
	}
	return a, a.Validate()
}

func FindDisk(disks []Disk, ref string) (Disk, error) {
	ref = strings.TrimSpace(ref)
	for _, d := range disks {
		if ref == d.Name || ref == d.Path || (d.ByID != "" && ref == d.ByID) {
			return d, nil
		}
	}
	return Disk{}, fmt.Errorf("диск %q не найден", ref)
}
