package update

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func makeBundle(t *testing.T, files map[string]string, corrupt string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	var sums bytes.Buffer
	for name, body := range files {
		h := sha256.Sum256([]byte(body))
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
		if name == corrupt {
			body += "!"
		}
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.WriteHeader(&tar.Header{Name: "SHA256SUMS", Mode: 0o644, Size: int64(sums.Len()), Typeflag: tar.TypeReg})
	_, _ = tw.Write(sums.Bytes())
	_ = tw.Close()
	return buf.Bytes()
}

var goodFiles = map[string]string{
	"manifest.json":            `{"product":"rendolnet","version":"1.2.0","images":["a"]}`,
	"images.tar.zst":           "images",
	"manifests/rendolnet.yaml": "kind: List",
}

func TestExtractOK(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "b")
	m, err := Extract(bytes.NewReader(makeBundle(t, goodFiles, "")), dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.2.0" {
		t.Fatalf("версия %q", m.Version)
	}
	if err := Verify(dir); err != nil {
		t.Fatal(err)
	}
}

func TestExtractCorrupt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "b")
	_, err := Extract(bytes.NewReader(makeBundle(t, goodFiles, "images.tar.zst")), dir)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("повреждение не замечено: %v", err)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	files := map[string]string{"../evil": "x", "manifest.json": goodFiles["manifest.json"]}
	if _, err := Extract(bytes.NewReader(makeBundle(t, files, "")), filepath.Join(t.TempDir(), "b")); err == nil {
		t.Fatal("путь с .. принят")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.10", "1.2.9", 1},
		{"1.2.0", "1.2.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.2.0-5-gabc1234", "1.2.0", 1},
		{"0.9", "1.0", -1},
		{"1.10.0", "1.9.99", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, ожидалось %d", c.a, c.b, got, c.want)
		}
	}
}
