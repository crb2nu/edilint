package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionAndNotes(t *testing.T) {
	for _, tag := range []string{"v0.2.0", "v1.20.3", "v10.0.0"} {
		if _, err := version(tag); err != nil {
			t.Fatal(err)
		}
	}
	for _, tag := range []string{"0.2.0", "v01.2.0", "v1.2", "v1.2.3-rc.1", "v1.2.3+build", "v999999999999999999999.0.0", "v1.2.3\n"} {
		if _, err := version(tag); err == nil {
			t.Errorf("accepted malformed version %q", tag)
		}
	}
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{"## [Unreleased]\n- future\n## [0.2.0] - 2026-09-18\n### Added\n- release\n## [0.1.0] - 2026-08-15\n- old", true},
		{"## [0.2.0] - 2026-09-18\n", false},
		{"## [0.2.0] - 2026-02-30\n- release", false},
		{"## [0.2.0] - 2026-09-18\n- release\n## [0.2.0] - 2026-09-18\n- duplicate", false},
		{"## [Unreleased]\n- release", false},
	} {
		notes, err := releaseNotes(tc.body, "v0.2.0")
		if (err == nil) != tc.ok {
			t.Errorf("notes %q: %v", tc.body, err)
		}
		if tc.ok && (strings.Contains(notes, "future") || strings.Contains(notes, "old") || !strings.Contains(notes, "release")) {
			t.Errorf("wrong notes section: %q", notes)
		}
	}
}

func TestTagGate(t *testing.T) {
	dir := t.TempDir()
	gitTest := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitTest("init", "-b", "main")
	gitTest("config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	gitTest("config", "commit.gpgsign", "false")
	gitTest("config", "tag.gpgsign", "false")
	gitTest("config", "user.name", "Release test")
	gitTest("config", "user.email", "test@example.invalid")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.invalid/test\n\ngo 1.23\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("## [0.2.0] - 2026-09-18\n- A tested release.\n"), 0o600)
	gitTest("add", ".")
	gitTest("commit", "-m", "test fixture")
	sha := gitTest("rev-parse", "HEAD")
	gitTest("tag", "-a", "v0.2.0", "-m", "release")
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if _, err = checkTag("v0.2.0", sha, "main"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tag, sha, main string }{
		{"v0.2.0", "short", "main"},
		{"v0.2.0", strings.Repeat("a", 40), "main"},
		{"v0.3.0", sha, "main"},
	} {
		if _, err = checkTag(tc.tag, tc.sha, tc.main); err == nil {
			t.Errorf("accepted invalid release input %+v", tc)
		}
	}
	gitTest("tag", "v0.3.0")
	if _, err = checkTag("v0.2.0", sha, "main"); err == nil {
		t.Error("accepted a version older than another tag")
	}
	gitTest("tag", "-d", "v0.3.0")
	os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("dirty"), 0o600)
	if _, err = checkTag("v0.2.0", sha, "main"); err == nil {
		t.Error("accepted dirty release files")
	}
	gitTest("checkout", "--", "CHANGELOG.md")
	gitTest("tag", "v2.0.0")
	if _, err = checkTag("v2.0.0", sha, "main"); err == nil || !strings.Contains(err.Error(), "module path") {
		t.Errorf("accepted a major release without its module suffix: %v", err)
	}
	gitTest("tag", "-d", "v2.0.0")
	gitTest("checkout", "-b", "unmerged")
	gitTest("commit", "--allow-empty", "-m", "unmerged change")
	sha = gitTest("rev-parse", "HEAD")
	gitTest("tag", "v0.2.1")
	if _, err = checkTag("v0.2.1", sha, "main"); err == nil {
		t.Error("accepted an off-main release")
	}
}

func TestCIGate(t *testing.T) {
	sha := strings.Repeat("a", 40)
	good := workflowRun{1, sha, "main", "push", "completed", "success"}
	for _, tc := range []struct {
		name string
		runs []workflowRun
		ok   bool
	}{
		{"success", []workflowRun{good}, true},
		{"missing", nil, false},
		{"wrong commit", []workflowRun{{1, strings.Repeat("b", 40), "main", "push", "completed", "success"}}, false},
		{"pull request", []workflowRun{{1, sha, "main", "pull_request", "completed", "success"}}, false},
		{"wrong branch", []workflowRun{{1, sha, "feature", "push", "completed", "success"}}, false},
		{"newer failed", []workflowRun{good, {2, sha, "main", "push", "completed", "failure"}}, false},
		{"newer pending", []workflowRun{good, {2, sha, "main", "push", "in_progress", ""}}, false},
		{"rerun pending", []workflowRun{{1, sha, "main", "push", "in_progress", ""}}, false},
		{"skipped", []workflowRun{{1, sha, "main", "push", "completed", "skipped"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := successfulRun(tc.runs, sha, "main"); (err == nil) != tc.ok {
				t.Fatalf("successfulRun: %v", err)
			}
		})
	}
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/actions/workflows/ci.yml/runs" || r.URL.Query().Get("head_sha") != sha || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("wrong evidence request: %s", r.URL)
				}
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []workflowRun{good}})
			}))
			defer srv.Close()
			if err := checkCI(srv.URL, "owner/repo", sha, "main", "test-token"); (err == nil) != (status == http.StatusOK) {
				t.Fatalf("checkCI: %v", err)
			}
		})
	}
}

func writeArchives(t *testing.T, dir string, omit string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"version":"0.2.0"}`), 0o600)
	var sums strings.Builder
	for _, targetOS := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext, binary := ".tar.gz", "edilint"
			if targetOS == "windows" {
				ext, binary = ".zip", "edilint.exe"
			}
			name := fmt.Sprintf("edilint_0.2.0_%s_%s%s", targetOS, arch, ext)
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			members := []string{binary, "LICENSE", "README.md", "CHANGELOG.md"}
			if ext == ".zip" {
				zw := zip.NewWriter(f)
				for _, member := range members {
					if member != omit {
						w, createErr := zw.Create(member)
						if createErr != nil {
							t.Fatal(createErr)
						}
						fmt.Fprint(w, "fixture")
					}
				}
				zw.Close()
			} else {
				gz := gzip.NewWriter(f)
				tw := tar.NewWriter(gz)
				for _, member := range members {
					if member != omit {
						mode := int64(0o755)
						if omit == "nonexecutable" {
							mode = 0o644
						}
						tw.WriteHeader(&tar.Header{Name: member, Mode: mode, Size: 7, Typeflag: tar.TypeReg})
						tw.Write([]byte("fixture"))
					}
				}
				tw.Close()
				gz.Close()
			}
			f.Close()
			b, readErr := os.ReadFile(filepath.Join(dir, name))
			if readErr != nil {
				t.Fatal(readErr)
			}
			fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(b), name)
		}
	}
	os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums.String()), 0o600)
}

func TestArchiveGate(t *testing.T) {
	for _, scenario := range []string{"valid", "missing documentation", "corrupt archive", "missing checksum", "missing target", "nonexecutable"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			omit := ""
			switch scenario {
			case "missing documentation":
				omit = "LICENSE"
			case "nonexecutable":
				omit = scenario
			}
			writeArchives(t, dir, omit)
			switch scenario {
			case "corrupt archive":
				os.WriteFile(filepath.Join(dir, "edilint_0.2.0_linux_amd64.tar.gz"), []byte("corrupt"), 0o600)
			case "missing checksum":
				os.WriteFile(filepath.Join(dir, "checksums.txt"), nil, 0o600)
			case "missing target":
				os.Remove(filepath.Join(dir, "edilint_0.2.0_windows_arm64.zip"))
			}
			v, native, err := verifyArtifacts(dir)
			if scenario == "valid" {
				if err != nil || v != "0.2.0" || len(native) == 0 {
					t.Fatalf("valid artifacts: %q %v", v, err)
				}
			} else if err == nil {
				t.Fatal("invalid artifacts accepted")
			}
		})
	}
}

func TestMirrorKeepsRefsSeparate(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("mirror runs in a POSIX GitLab runner")
	}
	script, err := filepath.Abs("../../scripts/mirror-github.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seed, remote := filepath.Join(dir, "seed"), filepath.Join(dir, "remote.git")
	gitAt := func(cwd string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		out, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			t.Fatalf("git %v: %v: %s", args, cmdErr, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitAt(dir, "init", "--bare", remote)
	gitAt(dir, "init", "-b", "main", seed)
	gitAt(seed, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	gitAt(seed, "config", "commit.gpgsign", "false")
	gitAt(seed, "config", "tag.gpgsign", "false")
	gitAt(seed, "config", "user.name", "Mirror test")
	gitAt(seed, "config", "user.email", "test@example.invalid")
	gitAt(seed, "config", "url."+filepath.ToSlash(remote)+".insteadOf", "https://github.com/crb2nu/edilint.git")
	gitAt(seed, "commit", "--allow-empty", "-m", "first")
	first := gitAt(seed, "rev-parse", "HEAD")
	gitAt(seed, "tag", "v0.1.0")
	gitAt(seed, "push", remote, "main")
	gitAt(seed, "commit", "--allow-empty", "-m", "second")
	second := gitAt(seed, "rev-parse", "HEAD")
	gitAt(seed, "tag", "v0.2.0")
	gitAt(seed, "tag", "v9.0.0")
	mirror := func(sha, tag, token string, wantOK bool) {
		t.Helper()
		cmd := exec.Command("sh", script)
		cmd.Dir = seed
		cmd.Env = append(os.Environ(), "GITHUB_MIRROR_TOKEN="+token, "CI_COMMIT_SHA="+sha,
			"CI_COMMIT_TAG="+tag, "CI_DEFAULT_BRANCH=main", "CI_COMMIT_BRANCH=main")
		out, cmdErr := cmd.CombinedOutput()
		if (cmdErr == nil) != wantOK {
			t.Fatalf("mirror sha=%s tag=%s: %v: %s", sha, tag, cmdErr, out)
		}
	}
	mirror(second, "", "test-token", true)
	if got := gitAt(remote, "tag", "--list"); got != "" {
		t.Fatalf("main job mirrored tags: %s", got)
	}
	mirror(first, "v0.1.0", "test-token", true)
	mirror(first, "", "test-token", true)
	if got := gitAt(remote, "rev-parse", "main"); got != second {
		t.Fatal("tag or stale main job rewound main")
	}
	if got := gitAt(remote, "tag", "--list"); got != "v0.1.0" {
		t.Fatalf("mirrored unvalidated tags: %s", got)
	}
	mirror(first, "v0.2.0", "test-token", false)
	mirror(second, "", "", false)
}
