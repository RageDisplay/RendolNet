// Package update — формат пакета обновления и его проверка.
//
// Пакет — tar со следующим содержимым:
//
//	manifest.json        версия, состав
//	images.tar.zst       образы контейнеров (docker save + zstd)
//	manifests/*.yaml     манифесты k3s
//	host/...             файлы узла (rendol-updater, юниты systemd), необязательно
//	SHA256SUMS           суммы всех файлов выше
//
// Подписей нет намеренно: проект открытый, собрать пакет может кто угодно.
// SHA256SUMS ловит повреждение при загрузке, а доверие к источнику — дело
// администратора, который пакет загружает.
package update

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const Product = "rendolnet"

type Manifest struct {
	Product    string            `json:"product"`
	Version    string            `json:"version"`
	Created    time.Time         `json:"created"`
	MinVersion string            `json:"min_version,omitempty"`
	Images     []string          `json:"images"`
	K3s        string            `json:"k3s,omitempty"`
	Notes      string            `json:"notes,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
}

var (
	ErrNotBundle = errors.New("update: это не пакет RendolNet")
	ErrChecksum  = errors.New("update: контрольная сумма не совпала")
)

// Extract распаковывает пакет в dir, проверяя суммы. При любой ошибке dir
// удаляется: наполовину распакованный пакет хуже отсутствующего.
func Extract(r io.Reader, dir string) (m *Manifest, err error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()

	got := map[string]string{}
	var sums []byte
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotBundle, err)
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." || strings.HasPrefix(name, "..") || path.IsAbs(name) {
			return nil, fmt.Errorf("update: недопустимый путь %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("update: в пакете допустимы только обычные файлы: %q", h.Name)
		}
		if name == "SHA256SUMS" {
			sums, err = io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return nil, err
			}
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o600)
		if err != nil {
			return nil, err
		}
		hs := sha256.New()
		_, err = io.Copy(io.MultiWriter(f, hs), tr)
		cerr := f.Close()
		if err != nil {
			return nil, err
		}
		if cerr != nil {
			return nil, cerr
		}
		got[name] = hex.EncodeToString(hs.Sum(nil))
	}
	if sums == nil {
		return nil, fmt.Errorf("%w: нет SHA256SUMS", ErrNotBundle)
	}
	want, err := parseSums(sums)
	if err != nil {
		return nil, err
	}
	for name, sum := range want {
		if got[name] != sum {
			return nil, fmt.Errorf("%w: %s", ErrChecksum, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			return nil, fmt.Errorf("update: файл %s не упомянут в SHA256SUMS", name)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), sums, 0o640); err != nil {
		return nil, err
	}
	return ReadManifest(dir)
}

func ReadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: нет manifest.json", ErrNotBundle)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest.json: %w", ErrNotBundle, err)
	}
	if m.Product != Product || m.Version == "" {
		return nil, ErrNotBundle
	}
	return &m, nil
}

// Verify перепроверяет суммы уже распакованного пакета. Вызывается на узле
// перед применением: между загрузкой и установкой каталог мог измениться.
func Verify(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	want, err := parseSums(b)
	if err != nil {
		return err
	}
	for name, sum := range want {
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("update: %s: %w", name, err)
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != sum {
			return fmt.Errorf("%w: %s", ErrChecksum, name)
		}
	}
	return nil
}

// parseSums читает формат sha256sum: "<hex>  <имя>", имя может начинаться с *.
func parseSums(b []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		sum, name, ok := strings.Cut(line, " ")
		if !ok || len(sum) != 64 {
			return nil, fmt.Errorf("update: строка SHA256SUMS: %q", line)
		}
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		name = path.Clean(strings.TrimPrefix(name, "./"))
		out[name] = strings.ToLower(sum)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("update: пустой SHA256SUMS")
	}
	return out, sc.Err()
}
