package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var stableTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func git(args ...string) (string, error) {
	b, err := exec.Command("git", args...).CombinedOutput() // #nosec G702 -- Fixed Git subcommands and separate arguments; no shell evaluation.
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, b)
	}
	return strings.TrimSpace(string(b)), nil
}

func version(tag string) ([3]uint64, error) {
	var result [3]uint64
	m := stableTag.FindStringSubmatch(tag)
	if m == nil {
		return result, fmt.Errorf("%q must be a stable vMAJOR.MINOR.PATCH tag", tag)
	}
	for i := range result {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return result, fmt.Errorf("invalid version %q: %w", tag, err)
		}
		result[i] = n
	}
	return result, nil
}

func newer(a, b [3]uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func releaseNotes(changelog, tag string) (string, error) {
	pattern := `(?m)^## \[` + regexp.QuoteMeta(strings.TrimPrefix(tag, "v")) + `\] - (\d{4}-\d{2}-\d{2})\r?$`
	re := regexp.MustCompile(pattern)
	locs := re.FindAllStringSubmatchIndex(changelog, -1)
	if len(locs) != 1 {
		return "", fmt.Errorf("CHANGELOG.md needs exactly one dated section for %s", tag)
	}
	loc := locs[0]
	if _, err := time.Parse("2006-01-02", changelog[loc[2]:loc[3]]); err != nil {
		return "", fmt.Errorf("invalid changelog date: %w", err)
	}
	body := changelog[loc[1]:]
	if end := strings.Index(body, "\n## "); end >= 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "\n- ") {
		return "", fmt.Errorf("%s changelog section must contain release notes", tag)
	}
	return strings.TrimSpace(body) + "\n", nil
}

func checkTag(tag, sha, mainRef string) (string, error) {
	v, err := version(tag)
	if err != nil {
		return "", err
	}
	if !commitSHA.MatchString(sha) {
		return "", fmt.Errorf("expected a full commit SHA")
	}
	for _, ref := range []string{"HEAD", "refs/tags/" + tag + "^{commit}"} {
		actual, gitErr := git("rev-parse", "--verify", ref)
		if gitErr != nil {
			return "", gitErr
		}
		if actual != sha {
			return "", fmt.Errorf("%s resolves to %s, expected %s", ref, actual, sha)
		}
	}
	if _, err = git("merge-base", "--is-ancestor", sha, mainRef); err != nil {
		return "", fmt.Errorf("release commit must belong to %s: %w", mainRef, err)
	}
	if _, err = git("diff", "--quiet", "HEAD", "--"); err != nil {
		return "", fmt.Errorf("tracked release files are dirty: %w", err)
	}
	tags, err := git("tag", "--list", "v*")
	if err != nil {
		return "", err
	}
	for _, other := range strings.Fields(tags) {
		ov, parseErr := version(other)
		if parseErr == nil && newer(ov, v) {
			return "", fmt.Errorf("%s is older than existing tag %s", tag, other)
		}
	}
	module, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	modulePath := ""
	for _, line := range strings.Split(string(module), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			modulePath = strings.Trim(fields[1], `"`)
			break
		}
	}
	if v[0] >= 2 && !strings.HasSuffix(modulePath, fmt.Sprintf("/v%d", v[0])) {
		return "", fmt.Errorf("major version %d requires a matching Go module path suffix", v[0])
	}
	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		return "", err
	}
	return releaseNotes(string(changelog), tag)
}
