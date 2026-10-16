package update

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Host — всё, что установщик обновления делает с узлом. Пути вынесены сюда,
// чтобы тест мог прогнать применение во временном каталоге.
type Host struct {
	Root        string // каталог обмена с rendol-api
	FSRoot      string // корень файловой системы узла, "/" в работе
	ManifestDir string // куда k3s смотрит за манифестами
	K3s         string // путь к k3s
	HealthURL   string
	Timeout     time.Duration
	Log         func(string)
}

func DefaultHost() *Host {
	return &Host{
		Root:        DefaultRoot,
		FSRoot:      "/",
		ManifestDir: "/var/lib/rancher/k3s/server/manifests",
		K3s:         "/usr/local/bin/k3s",
		HealthURL:   "https://127.0.0.1/healthz",
		Timeout:     5 * time.Minute,
	}
}

const manifestName = "rendolnet.yaml"

type step struct {
	h  *Host
	st *Status
}

func (s *step) set(name string) {
	s.st.Step = name
	s.log(name)
}

func (s *step) log(line string) {
	s.st.Log = append(s.st.Log, time.Now().Format("15:04:05 ")+line)
	if s.h.Log != nil {
		s.h.Log(line)
	}
	_ = WriteJSON(filepath.Join(s.h.Root, StatusFile), s.st)
}

// Apply ставит пакет из staging. При любой ошибке после замены манифестов
// возвращает прежние манифесты и файлы узла.
func (h *Host) Apply(ctx context.Context, req *Request) (err error) {
	now := time.Now()
	st := &Status{RequestID: req.ID, Action: ActionUpdate, State: StateRunning, Version: req.Version, Started: &now}
	st.From = h.InstalledVersion()
	s := &step{h: h, st: st}

	finish := func(state State, msg string) {
		t := time.Now()
		st.State, st.Message, st.Finished = state, msg, &t
		s.log(msg)
	}

	staged := filepath.Join(h.Root, StagingDir)
	s.set("проверка пакета")
	if err := Verify(staged); err != nil {
		finish(StateFailed, err.Error())
		return err
	}
	m, err := ReadManifest(staged)
	if err != nil {
		finish(StateFailed, err.Error())
		return err
	}
	if m.Version != req.Version {
		err := fmt.Errorf("в staging версия %s, а запрошена %s", m.Version, req.Version)
		finish(StateFailed, err.Error())
		return err
	}

	s.set("загрузка образов")
	if err := h.importImages(ctx, filepath.Join(staged, "images.tar.zst")); err != nil {
		finish(StateFailed, "образы: "+err.Error())
		return err
	}

	backup := filepath.Join(h.Root, "backup", st.From)
	_ = os.RemoveAll(backup)
	s.set("резервная копия")
	if err := copyFile(filepath.Join(h.ManifestDir, manifestName), filepath.Join(backup, "manifests", manifestName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		finish(StateFailed, "резервная копия манифестов: "+err.Error())
		return err
	}

	s.set("файлы узла")
	installed, err := h.installHostFiles(filepath.Join(staged, "host"), filepath.Join(backup, "host"))
	rollback := func(reason string) error {
		s.log("откат: " + reason)
		for _, rel := range installed {
			src := filepath.Join(backup, "host", rel)
			dst := filepath.Join(h.FSRoot, rel)
			if _, err := os.Stat(src); err == nil {
				_ = atomicCopy(src, dst)
			} else {
				_ = os.Remove(dst)
			}
		}
		if err := copyFile(filepath.Join(backup, "manifests", manifestName), filepath.Join(h.ManifestDir, manifestName)); err != nil {
			s.log("манифесты не восстановлены: " + err.Error())
		}
		finish(StateRolledBack, reason)
		return errors.New(reason)
	}
	if err != nil {
		return rollback("файлы узла: " + err.Error())
	}

	s.set("манифесты")
	if err := atomicCopy(filepath.Join(staged, "manifests", manifestName), filepath.Join(h.ManifestDir, manifestName)); err != nil {
		return rollback("манифесты: " + err.Error())
	}

	s.set("ожидание новой версии")
	if err := h.waitHealthy(ctx, m.Version); err != nil {
		return rollback("новая версия не поднялась: " + err.Error())
	}

	_ = os.WriteFile(filepath.Join(h.Root, VersionFile), []byte(m.Version), 0o644)
	_ = os.RemoveAll(staged)
	finish(StateDone, "установлена версия "+m.Version)
	return nil
}

func (h *Host) InstalledVersion() string {
	b, err := os.ReadFile(filepath.Join(h.Root, VersionFile))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

// importImages распаковывает zstd на лету и отдаёт поток в containerd k3s.
func (h *Host) importImages(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer dec.Close()
	cmd := exec.CommandContext(ctx, h.K3s, "ctr", "-n", "k8s.io", "images", "import", "-")
	cmd.Stdin = dec
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// installHostFiles копирует host/... в корень узла, сохраняя прежние версии
// файлов в backup. Возвращает список установленных путей.
func (h *Host) installHostFiles(src, backup string) ([]string, error) {
	var done []string
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		dst := filepath.Join(h.FSRoot, rel)
		if _, err := os.Stat(dst); err == nil {
			if err := copyFile(dst, filepath.Join(backup, rel)); err != nil {
				return err
			}
		}
		if err := atomicCopy(p, dst); err != nil {
			return err
		}
		done = append(done, rel)
		return nil
	})
	return done, err
}

// waitHealthy ждёт, пока rendol-api новой версии ответит на /healthz.
func (h *Host) waitHealthy(ctx context.Context, version string) error {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	hc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	var last string
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.HealthURL, nil)
		if resp, err := hc.Do(req); err == nil {
			var v struct {
				Version string `json:"version"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if v.Version == version {
				return nil
			}
			last = "отвечает версия " + v.Version
		} else {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (%s)", ctx.Err(), last)
		case <-time.After(3 * time.Second):
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// atomicCopy пишет во временный файл рядом и переименовывает: работающий
// бинарь подменяется без «text file busy», а читатель не видит половину файла.
func atomicCopy(src, dst string) error {
	tmp := dst + ".rn-new"
	if err := copyFile(src, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
