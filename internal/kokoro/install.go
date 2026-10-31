// Package kokoro installs the opt-in Kokoro speech engine (sherpa-onnx with
// Kokoro-82M) and speaks project names with it, in the bundled clips' voice.
package kokoro

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// installID is the marker file's content: an install made for other pinned
// versions counts as not installed.
const installID = "sherpa-onnx " + EngineVersion + ", " + ModelName + "\n"

const markerName = "installed"

// Dir is the install directory under the state directory.
func Dir(stateDir string) string { return filepath.Join(stateDir, "kokoro") }

// Installed reports whether dir holds a complete install of the pinned versions.
func Installed(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, markerName))
	return err == nil && string(b) == installID
}

// Install downloads the engine for this platform and the model, verifies
// their SHA-256, and extracts what is needed into dir, replacing any previous
// install. With from set, the archives are read from that directory instead
// of downloaded. Progress goes to out.
func Install(dir, from string, out io.Writer) error {
	assets, err := Assets(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	return install(dir, from, assets, out)
}

func install(dir, from string, assets []Asset, out io.Writer) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".kokoro-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	root := filepath.Join(stage, "kokoro")

	for _, a := range assets {
		archive := filepath.Join(from, a.Name)
		if from == "" {
			archive = filepath.Join(stage, a.Name)
			if err := download(a, archive, out); err != nil {
				return err
			}
		}
		if err := verify(a, archive); err != nil {
			return err
		}
		if err := extract(a, archive, root); err != nil {
			return fmt.Errorf("%s: %w", a.Name, err)
		}
		if from == "" {
			os.Remove(archive)
		}
	}
	if err := os.WriteFile(filepath.Join(root, markerName), []byte(installID), 0o600); err != nil {
		return err
	}

	// Swap the new install in; a previous one moves into stage and goes with it.
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, filepath.Join(stage, "previous")); err != nil {
			return fmt.Errorf("replace %s: %w", dir, err)
		}
	}
	if err := os.Rename(root, dir); err != nil {
		os.Rename(filepath.Join(stage, "previous"), dir)
		return err
	}
	return nil
}

// Remove deletes the install in dir.
func Remove(dir string) error { return os.RemoveAll(dir) }

func download(a Asset, dest string, out io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "cnw")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", a.URL, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	p := &progress{out: out, name: a.Name, total: a.Size, last: -1}
	_, err = io.Copy(io.MultiWriter(f, p), resp.Body)
	fmt.Fprintln(out)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", a.Name, err)
	}
	return nil
}

// progress prints "name: N% of M MB" whenever the percentage changes.
type progress struct {
	out         io.Writer
	name        string
	total, done int64
	last        int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if pct := 100 * p.done / max(p.total, 1); pct != p.last {
		p.last = pct
		fmt.Fprintf(p.out, "\r%s: %d%% of %d MB", p.name, min(pct, 100), p.total/1_000_000)
	}
	return len(b), nil
}

func verify(a Asset, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("%s: SHA-256 %s, want %s", path, got, a.SHA256)
	}
	return nil
}

func extract(a Asset, archive, root string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTar(a, bzip2.NewReader(bufio.NewReader(f)), root)
}

// extractTar writes the regular files a wants from the tar stream r below
// root/a.into, and fails if any of a.files is missing.
func extractTar(a Asset, r io.Reader, root string) error {
	found := map[string]bool{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(hdr.Name, a.top+"/")
		if !ok || hdr.Typeflag != tar.TypeReg || !a.wants(rel) {
			continue
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("unsafe path %q in archive", hdr.Name)
		}
		dest := filepath.Join(root, a.into, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if hdr.Mode&0o111 != 0 {
			mode = 0o755
		}
		w, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, tr)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		found[rel] = true
	}
	for _, f := range a.files {
		if !found[f] {
			return fmt.Errorf("%s missing from the archive", f)
		}
	}
	return nil
}
