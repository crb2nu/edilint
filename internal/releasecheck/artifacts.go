package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/crb2nu/edilint"
)

// nativeEntry accepts the members of a CLI archive.
func nativeEntry(name string) bool {
	return name == "edilint" || name == "edilint.exe" || name == "LICENSE" || name == "README.md" || name == "CHANGELOG.md"
}

// wasmEntry accepts the members of the browser archive: the module, the Go
// runtime's JavaScript shim, the license, and the sample fixtures a site can
// offer without a checkout of this repository.
var wasmFixture = regexp.MustCompile(`^testdata/[A-Za-z0-9][A-Za-z0-9._-]*$`)

func wasmEntry(name string) bool {
	return name == "edilint.wasm" || name == "wasm_exec.js" || name == "LICENSE" || wasmFixture.MatchString(name)
}

func archiveFiles(path string) (map[string][]byte, error) {
	return archiveMembers(path, nativeEntry)
}

func archiveMembers(path string, allowed func(string) bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	add := func(name string, r io.Reader) error {
		if !allowed(name) {
			return fmt.Errorf("unexpected archive entry %q", name)
		}
		if _, exists := files[name]; exists {
			return fmt.Errorf("duplicate archive entry %q", name)
		}
		b, err := io.ReadAll(io.LimitReader(r, 32<<20+1))
		if err != nil {
			return err
		}
		if len(b) == 0 || len(b) > 32<<20 {
			return fmt.Errorf("archive entry %q has unexpected size", name)
		}
		files[name] = b
		return nil
	}
	if strings.HasSuffix(path, ".zip") {
		z, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = z.Close() }()
		for _, f := range z.File {
			if !f.Mode().IsRegular() {
				return nil, fmt.Errorf("non-regular archive entry %q", f.Name)
			}
			r, openErr := f.Open()
			if openErr != nil {
				return nil, openErr
			}
			err = add(f.Name, r)
			_ = r.Close()
			if err != nil {
				return nil, err
			}
		}
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		tr := tar.NewReader(gz)
		for {
			h, nextErr := tr.Next()
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				return nil, nextErr
			}
			if h.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("non-regular archive entry %q", h.Name)
			}
			if h.Name == "edilint" && h.Mode&0o111 == 0 {
				return nil, fmt.Errorf("archive binary is not executable")
			}
			if err = add(h.Name, tr); err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func verifyArtifacts(dir string) (string, []byte, error) {
	metadata, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return "", nil, err
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err = json.Unmarshal(metadata, &meta); err != nil {
		return "", nil, err
	}
	if !regexp.MustCompile(`^[0-9][A-Za-z0-9.+-]*$`).MatchString(meta.Version) {
		return "", nil, fmt.Errorf("invalid artifact version %q", meta.Version)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return "", nil, err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 || sums[fields[1]] != "" {
			return "", nil, fmt.Errorf("malformed or duplicate checksum entry")
		}
		sums[fields[1]] = fields[0]
	}
	if len(sums) != 7 {
		return "", nil, fmt.Errorf("expected seven release archive checksums (six CLI targets and the browser module), got %d", len(sums))
	}
	if err = verifyWasmArchive(dir, meta.Version, sums); err != nil {
		return "", nil, err
	}
	var native []byte
	for _, targetOS := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext, binary := ".tar.gz", "edilint"
			if targetOS == "windows" {
				ext, binary = ".zip", "edilint.exe"
			}
			name := fmt.Sprintf("edilint_%s_%s_%s%s", meta.Version, targetOS, arch, ext)
			path := filepath.Join(dir, name)
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", nil, readErr
			}
			if fmt.Sprintf("%x", sha256.Sum256(b)) != sums[name] {
				return "", nil, fmt.Errorf("checksum mismatch for %s", name)
			}
			files, archiveErr := archiveFiles(path)
			if archiveErr != nil {
				return "", nil, fmt.Errorf("%s: %w", name, archiveErr)
			}
			for _, required := range []string{binary, "LICENSE", "README.md", "CHANGELOG.md"} {
				if len(files[required]) == 0 || len(files) != 4 {
					return "", nil, fmt.Errorf("%s must contain the binary and all three release documents", name)
				}
			}
			if targetOS == runtime.GOOS && arch == runtime.GOARCH {
				native = files[binary]
			}
		}
	}
	if native == nil {
		return "", nil, fmt.Errorf("no archive for this runner's %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return meta.Version, native, nil
}

// verifyWasmArchive checks the browser build that ships beside the CLI
// archives: a WebAssembly module, the matching wasm_exec.js shim, the license,
// and at least one sample fixture, all under the published checksum.
func verifyWasmArchive(dir, version string, sums map[string]string) error {
	name := fmt.Sprintf("edilint_%s_wasm.tar.gz", version)
	path := filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != sums[name] {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	files, err := archiveMembers(path, wasmEntry)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	module := files["edilint.wasm"]
	if len(module) < 8 || string(module[:4]) != "\x00asm" {
		return fmt.Errorf("%s must contain a WebAssembly module", name)
	}
	if !strings.Contains(string(files["wasm_exec.js"]), "Go") {
		return fmt.Errorf("%s must contain the Go wasm_exec.js shim", name)
	}
	if len(files["LICENSE"]) == 0 {
		return fmt.Errorf("%s must contain the license", name)
	}
	fixtures := 0
	for member := range files {
		if wasmFixture.MatchString(member) {
			fixtures++
		}
	}
	if fixtures == 0 {
		return fmt.Errorf("%s must contain the sample fixtures", name)
	}
	return nil
}

func checkArtifacts(dir string) error {
	v, binary, err := verifyArtifacts(dir)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "edilint-release-smoke-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	path := filepath.Join(tmp, "edilint")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	// The purpose of this check is to execute the just-built release binary.
	// #nosec G306 -- This verified artifact must be executable for the smoke test.
	if err = os.WriteFile(path, binary, 0o700); err != nil {
		return err
	}
	runBinary := func(want int, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = tmp
		out, execErr := cmd.Output()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(execErr, &exitErr) {
			code = exitErr.ExitCode()
		} else if execErr != nil {
			return nil, execErr
		}
		if code != want {
			return nil, fmt.Errorf("packaged CLI %v exited %d, expected %d: %s", args, code, want, out)
		}
		return out, nil
	}
	out, err := runBinary(0, "--version")
	if err != nil {
		return err
	}
	if string(out) != "edilint "+v+"\n" {
		return fmt.Errorf("packaged version %q does not match %q", out, v)
	}
	for _, tc := range []struct {
		name, content string
		code          int
	}{
		{"clean.txt", "ABC|one\n", 0},
		{"broken.txt", "ABC|o\x00ne\n", 1},
	} {
		file := filepath.Join(tmp, tc.name)
		if err = os.WriteFile(file, []byte(tc.content), 0o600); err != nil {
			return err
		}
		out, err = runBinary(tc.code, "--no-config", "--format", "text", "--json", file)
		if err != nil {
			return err
		}
		var report edilint.RunReport
		if err = json.Unmarshal(out, &report); err != nil {
			return err
		}
		if report.Version != edilint.SchemaVersion || len(report.Files) != 1 || (report.Summary.Total == 0) != (tc.code == 0) {
			return fmt.Errorf("unexpected packaged CLI JSON for %s: %s", tc.name, out)
		}
	}
	_, err = runBinary(2, "--no-config", filepath.Join(tmp, "missing.txt"))
	return err
}
