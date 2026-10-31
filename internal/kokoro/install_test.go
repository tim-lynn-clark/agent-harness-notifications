package kokoro

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fixtures describe testdata/fake-*.tar.bz2, which hold a few wanted and a few
// unwanted files each.
func fixtures(t *testing.T) []Asset {
	return []Asset{
		{Name: "fake-engine.tar.bz2", SHA256: fileSHA(t, "testdata/fake-engine.tar.bz2"),
			top: "fake-engine", files: []string{"bin/tool"}},
		{Name: "fake-model.tar.bz2", SHA256: fileSHA(t, "testdata/fake-model.tar.bz2"),
			top: "fake-model", files: []string{"m.onnx"}, dirs: []string{"espeak-ng-data"}, into: "model"},
	}
}

func TestInstallFromDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "kokoro")
	if Installed(dir) {
		t.Fatal("an empty directory must not count as installed")
	}
	if err := install(dir, "testdata", fixtures(t), io.Discard); err != nil {
		t.Fatal(err)
	}
	if !Installed(dir) {
		t.Fatal("Installed is false after install")
	}
	for _, p := range []string{"bin/tool", "model/m.onnx", "model/espeak-ng-data/en_dict", "model/espeak-ng-data/voices/x"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("wanted file missing: %v", err)
		}
	}
	for _, p := range []string{"bin/extra", "model/lexicon-zh.txt"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("%s was extracted but not wanted", p)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "bin/tool")); fi.Mode()&0o100 == 0 {
			t.Errorf("executable lost its mode: %v", fi.Mode())
		}
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 1 {
		t.Errorf("staging left behind: %v", entries)
	}
}

func TestInstallReplacesPreviousInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kokoro")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "stale"), nil, 0o600)
	if err := install(dir, "testdata", fixtures(t), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale")); err == nil {
		t.Fatal("the previous install's files survived")
	}
}

func TestInstallChecksumMismatchKeepsPreviousInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kokoro")
	if err := install(dir, "testdata", fixtures(t), io.Discard); err != nil {
		t.Fatal(err)
	}
	assets := fixtures(t)
	assets[1].SHA256 = strings.Repeat("0", 64)
	err := install(dir, "testdata", assets, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	if !Installed(dir) {
		t.Fatal("a failed install must leave the previous one in place")
	}
}

func TestInstalledRejectsOtherVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, markerName), []byte("sherpa-onnx v0.0.1, other-model\n"), 0o600)
	if Installed(dir) {
		t.Fatal("an install of other pinned versions must not count")
	}
}

func tarOf(t *testing.T, names ...string) io.Reader {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, n := range names {
		if err := w.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x"))
	}
	w.Close()
	return &b
}

func TestExtractRejectsPathsLeavingTheInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	a := Asset{top: "top", dirs: []string{"espeak-ng-data"}}
	err := extractTar(a, tarOf(t, "top/espeak-ng-data/../../evil"), root)
	if err == nil {
		t.Fatal("a path escaping the install must fail")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evil")); err == nil {
		t.Fatal("the escaping file was written")
	}
}

func TestExtractRequiresListedFiles(t *testing.T) {
	a := Asset{top: "top", files: []string{"bin/tool", "lib/runtime"}}
	err := extractTar(a, tarOf(t, "top/bin/tool"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "lib/runtime") {
		t.Fatalf("err = %v, want lib/runtime reported missing", err)
	}
}

func TestAssetsCoverEveryBuildTarget(t *testing.T) {
	// The targets of scripts/build.sh.
	for _, target := range []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64", "windows/amd64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		if _, err := Assets(goos, goarch); err != nil {
			t.Errorf("%s: %v", target, err)
		}
	}
}
