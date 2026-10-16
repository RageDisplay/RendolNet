//go:build linux

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func stage(t *testing.T, root, version string) {
	t.Helper()
	dir := filepath.Join(root, StagingDir)
	var img bytes.Buffer
	zw, _ := zstd.NewWriter(&img)
	zw.Write([]byte("fake-images"))
	zw.Close()
	files := map[string][]byte{
		"manifest.json":                  []byte(fmt.Sprintf(`{"product":"rendolnet","version":%q,"images":["x"]}`, version)),
		"images.tar.zst":                 img.Bytes(),
		"manifests/rendolnet.yaml":       []byte("version: " + version),
		"host/usr/local/sbin/rendol-new": []byte("bin " + version),
	}
	var sums bytes.Buffer
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, body, 0o644)
		h := sha256.Sum256(body)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), sums.Bytes(), 0o644)
}

func testHost(t *testing.T, healthVersion string) (*Host, string) {
	tmp := t.TempDir()
	k3s := filepath.Join(tmp, "k3s")
	got := filepath.Join(tmp, "imported")
	os.WriteFile(k3s, []byte("#!/bin/sh\ncat > "+got+"\n"), 0o755)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":"ok","version":%q}`, healthVersion)
	}))
	t.Cleanup(srv.Close)
	h := &Host{
		Root: filepath.Join(tmp, "host"), FSRoot: filepath.Join(tmp, "fs"),
		ManifestDir: filepath.Join(tmp, "manifests"), K3s: k3s,
		HealthURL: srv.URL, Timeout: 2 * time.Second,
	}
	os.MkdirAll(h.ManifestDir, 0o755)
	os.WriteFile(filepath.Join(h.ManifestDir, manifestName), []byte("version: 1.0.0"), 0o644)
	os.MkdirAll(h.Root, 0o755)
	os.WriteFile(filepath.Join(h.Root, VersionFile), []byte("1.0.0"), 0o644)
	return h, got
}

func TestApplyOK(t *testing.T) {
	h, imported := testHost(t, "1.1.0")
	stage(t, h.Root, "1.1.0")
	if err := h.Apply(context.Background(), &Request{ID: "x", Action: ActionUpdate, Version: "1.1.0"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(imported); string(b) != "fake-images" {
		t.Fatalf("в k3s ушло %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(h.ManifestDir, manifestName)); string(b) != "version: 1.1.0" {
		t.Fatalf("манифест: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(h.FSRoot, "usr/local/sbin/rendol-new")); string(b) != "bin 1.1.0" {
		t.Fatalf("файл узла: %q", b)
	}
	if h.InstalledVersion() != "1.1.0" || ReadStatus(h.Root).State != StateDone {
		t.Fatalf("состояние: %s %+v", h.InstalledVersion(), ReadStatus(h.Root))
	}
}

func TestApplyRollsBack(t *testing.T) {
	h, _ := testHost(t, "1.0.0") // новая версия так и не ответила
	stage(t, h.Root, "1.1.0")
	if err := h.Apply(context.Background(), &Request{ID: "x", Action: ActionUpdate, Version: "1.1.0"}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if b, _ := os.ReadFile(filepath.Join(h.ManifestDir, manifestName)); string(b) != "version: 1.0.0" {
		t.Fatalf("манифест не откатился: %q", b)
	}
	if _, err := os.Stat(filepath.Join(h.FSRoot, "usr/local/sbin/rendol-new")); err == nil {
		t.Fatal("новый файл узла остался после отката")
	}
	if st := ReadStatus(h.Root); st.State != StateRolledBack || h.InstalledVersion() != "1.0.0" {
		t.Fatalf("состояние: %+v", st)
	}
}
