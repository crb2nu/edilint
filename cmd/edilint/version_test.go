package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	settings := func(kv ...string) []debug.BuildSetting {
		var out []debug.BuildSetting
		for i := 0; i+1 < len(kv); i += 2 {
			out = append(out, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return out
	}
	tests := []struct {
		name string
		set  string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"ldflags win", "0.3.0", &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, true, "0.3.0"},
		{"no build info", "dev", nil, false, "dev"},
		{"go install of a tag", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}}, true, "0.3.0"},
		{"checkout with commit", "dev", &debug.BuildInfo{
			Main:     debug.Module{Version: "(devel)"},
			Settings: settings("vcs.revision", "0123456789abcdef0123", "vcs.modified", "false"),
		}, true, "dev (0123456789ab)"},
		{"dirty checkout", "dev", &debug.BuildInfo{
			Settings: settings("vcs.revision", "abc123", "vcs.modified", "true"),
		}, true, "dev (abc123-dirty)"},
		{"checkout without vcs", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFrom(tt.set, tt.info, tt.ok); got != tt.want {
				t.Errorf("versionFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionFlagReportsBuildVersion(t *testing.T) {
	code, stdout, _ := exec("--version")
	if code != exitClean {
		t.Fatalf("exit = %d", code)
	}
	want := "edilint " + buildVersion() + "\n"
	if stdout != want {
		t.Errorf("--version printed %q, want %q", stdout, want)
	}
	if !strings.HasPrefix(stdout, "edilint ") {
		t.Errorf("--version output = %q", stdout)
	}
}
