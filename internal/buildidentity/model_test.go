package buildidentity

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func TestFromBuildInfoPreservesPublishedFactsAndReleaseEligibility(t *testing.T) {
	identity, err := FromBuildInfo(validBuildInfo())
	if err != nil {
		t.Fatalf("FromBuildInfo returned error: %v", err)
	}

	if identity.Version() != "v1.2.3" || identity.VCS() != "git" || identity.Revision() != testRevision {
		t.Fatalf("source identity = version=%q vcs=%q revision=%q", identity.Version(), identity.VCS(), identity.Revision())
	}
	revisionAt, ok := identity.RevisionTime()
	if !ok || revisionAt.Format(time.RFC3339) != "2026-07-01T02:03:04Z" {
		t.Fatalf("revision time = %v/%t", revisionAt, ok)
	}
	if identity.SourceState() != SourceClean || identity.SourceState().String() != "clean" {
		t.Fatalf("source state = %s", identity.SourceState())
	}
	if identity.GoVersion() != "go1.26.5" || identity.Target().String() != "linux/amd64" {
		t.Fatalf("build identity = go=%q target=%s", identity.GoVersion(), identity.Target())
	}
	if err := identity.RequireReleaseFacts(validReleaseRequirement()); err != nil {
		t.Fatalf("owned release facts were not preserved: %v", err)
	}
}

func TestFromBuildInfoLeavesAbsentFactsUnknown(t *testing.T) {
	info := debug.BuildInfo{
		Path:      MainPackagePath,
		GoVersion: "go1.26.5",
		Main:      debug.Module{Path: MainModulePath, Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "unowned", Value: "one"},
			{Key: "unowned", Value: "two"},
		},
	}
	identity, err := FromBuildInfo(info)
	if err != nil {
		t.Fatalf("FromBuildInfo returned error for absent facts: %v", err)
	}
	if identity.Version() != "(devel)" {
		t.Fatalf("development version = %q", identity.Version())
	}
	if identity.VCS() != "" || identity.Revision() != "" || identity.SourceState() != SourceUnknown {
		t.Fatalf("unknown source facts = vcs=%q revision=%q state=%s", identity.VCS(), identity.Revision(), identity.SourceState())
	}
	if _, ok := identity.RevisionTime(); ok {
		t.Fatal("missing revision time was reported as known")
	}
	if identity.Target().String() != "unknown/unknown" {
		t.Fatalf("unknown build target = %s", identity.Target())
	}
}

func TestFromBuildInfoRejectsMalformedOwnedSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*debug.BuildInfo)
		want   string
	}{
		{name: "duplicate", mutate: func(info *debug.BuildInfo) {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs", Value: "git"})
		}, want: "duplicate vcs"},
		{name: "empty vcs", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs", "") }, want: "invalid vcs"},
		{name: "spaced revision", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.revision", " "+testRevision) }, want: "invalid vcs.revision"},
		{name: "malformed time", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.time", "not-time") }, want: "invalid vcs.time"},
		{name: "non utc time", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.time", "2026-07-01T03:03:04+01:00") }, want: "must be UTC"},
		{name: "unknown modified", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.modified", "unknown") }, want: "invalid vcs.modified"},
		{name: "noncanonical goos", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "GOOS", "Linux") }, want: "invalid target"},
		{name: "noncanonical goarch", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "GOARCH", "amd-64") }, want: "invalid target"},
		{name: "invalid cgo", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "CGO_ENABLED", "false") }, want: "invalid CGO_ENABLED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := validBuildInfo()
			test.mutate(&info)
			_, err := FromBuildInfo(info)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("FromBuildInfo error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestFromBuildInfoTreatsPartialTargetAsUnknownButValidatesPresentFact(t *testing.T) {
	info := validBuildInfo()
	removeBuildSetting(&info, "GOARCH")
	identity, err := FromBuildInfo(info)
	if err != nil {
		t.Fatalf("FromBuildInfo returned error: %v", err)
	}
	if identity.Target().String() != "unknown/unknown" {
		t.Fatalf("partial target = %s, want unknown/unknown", identity.Target())
	}

	setBuildSetting(&info, "GOOS", "Linux")
	if _, err := FromBuildInfo(info); err == nil {
		t.Fatal("partial malformed target succeeded")
	}
}

func TestReleaseEligibilityAcceptsExactCleanIdentity(t *testing.T) {
	identity, err := FromBuildInfo(validBuildInfo())
	if err != nil {
		t.Fatalf("FromBuildInfo returned error: %v", err)
	}
	requirement := validReleaseRequirement()
	if err := identity.RequireReleaseFacts(requirement); err != nil {
		t.Fatalf("RequireReleaseFacts returned error: %v", err)
	}

	info := validBuildInfo()
	info.Main.Version = "v1.2.3-rc.1"
	identity, err = FromBuildInfo(info)
	if err != nil {
		t.Fatalf("FromBuildInfo returned error for prerelease: %v", err)
	}
	requirement, err = NewReleaseRequirement("v1.2.3-rc.1", testRevision, "go1.26.5")
	if err != nil {
		t.Fatalf("NewReleaseRequirement returned error: %v", err)
	}
	if err := identity.RequireReleaseFacts(requirement); err != nil {
		t.Fatalf("prerelease identity was rejected: %v", err)
	}
}

func TestReleaseEligibilityRejectsEveryWeakerIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*debug.BuildInfo)
	}{
		{name: "wrong main package", mutate: func(info *debug.BuildInfo) { info.Path = "example.com/other/cmd/daem" }},
		{name: "missing main package", mutate: func(info *debug.BuildInfo) { info.Path = "" }},
		{name: "wrong main module", mutate: func(info *debug.BuildInfo) { info.Main.Path = "example.com/other" }},
		{name: "development version", mutate: func(info *debug.BuildInfo) { info.Main.Version = "(devel)" }},
		{name: "pseudo version", mutate: func(info *debug.BuildInfo) { info.Main.Version = "v0.0.0-20260701020304-0123456789ab" }},
		{name: "noncanonical version", mutate: func(info *debug.BuildInfo) { info.Main.Version = "v1.2" }},
		{name: "build metadata", mutate: func(info *debug.BuildInfo) { info.Main.Version = "v1.2.3+private" }},
		{name: "version mismatch", mutate: func(info *debug.BuildInfo) { info.Main.Version = "v1.2.4" }},
		{name: "wrong vcs", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs", "hg") }},
		{name: "missing vcs", mutate: func(info *debug.BuildInfo) { removeBuildSetting(info, "vcs") }},
		{name: "short revision", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.revision", "0123456789ab") }},
		{name: "uppercase revision", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.revision", strings.ToUpper(testRevision)) }},
		{name: "revision mismatch", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.revision", strings.Repeat("a", 40)) }},
		{name: "missing revision time", mutate: func(info *debug.BuildInfo) { removeBuildSetting(info, "vcs.time") }},
		{name: "modified source", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "vcs.modified", "true") }},
		{name: "unknown source", mutate: func(info *debug.BuildInfo) { removeBuildSetting(info, "vcs.modified") }},
		{name: "wrong go version", mutate: func(info *debug.BuildInfo) { info.GoVersion = "go1.25.12" }},
		{name: "missing go version", mutate: func(info *debug.BuildInfo) { info.GoVersion = "" }},
		{name: "missing target", mutate: func(info *debug.BuildInfo) { removeBuildSetting(info, "GOOS"); removeBuildSetting(info, "GOARCH") }},
		{name: "partial target", mutate: func(info *debug.BuildInfo) { removeBuildSetting(info, "GOARCH") }},
		{name: "cgo enabled", mutate: func(info *debug.BuildInfo) { setBuildSetting(info, "CGO_ENABLED", "1") }},
		{name: "cgo unknown", mutate: func(info *debug.BuildInfo) { removeBuildSetting(info, "CGO_ENABLED") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := validBuildInfo()
			test.mutate(&info)
			identity, err := FromBuildInfo(info)
			if err != nil {
				t.Fatalf("FromBuildInfo returned unexpected parse error: %v", err)
			}
			if err := identity.RequireReleaseFacts(validReleaseRequirement()); err == nil {
				t.Fatal("weaker identity passed release eligibility")
			}
		})
	}
}

func TestReleaseEligibilityRejectsInvalidExternalRequirements(t *testing.T) {
	identity, err := FromBuildInfo(validBuildInfo())
	if err != nil {
		t.Fatalf("FromBuildInfo returned error: %v", err)
	}
	tests := [][3]string{
		{},
		{"1.2.3", testRevision, "go1.26.5"},
		{"v1.2", testRevision, "go1.26.5"},
		{"v1.2.3+meta", testRevision, "go1.26.5"},
		{"v0.0.0-20260701020304-0123456789ab", testRevision, "go1.26.5"},
		{"v1.2.3", "short", "go1.26.5"},
		{"v1.2.3", testRevision, ""},
		{"v1.2.3", testRevision, " go1.26.5"},
		{"v1.2.3", testRevision, "not-go"},
	}
	for _, values := range tests {
		if _, err := NewReleaseRequirement(values[0], values[1], values[2]); err == nil {
			t.Fatalf("invalid requirement %#v succeeded", values)
		}
	}
	if err := identity.RequireReleaseFacts(ReleaseRequirement{}); err == nil {
		t.Fatal("zero release requirement succeeded")
	}
}

func TestZeroIdentityAndEnumsFailClosed(t *testing.T) {
	var identity Identity
	if identity.SourceState().String() != "unknown" || CGOUnknown.String() != "unknown" {
		t.Fatalf("zero identity states = %s/%s", identity.SourceState(), CGOUnknown)
	}
	if SourceState(255).String() != "unknown" || CGOState(255).String() != "unknown" {
		t.Fatal("unknown enum values did not fail closed")
	}
	if err := identity.RequireReleaseFacts(validReleaseRequirement()); err == nil {
		t.Fatal("zero identity passed release eligibility")
	}
}

func TestCurrentAlwaysPreservesRuntimeOwnedFacts(t *testing.T) {
	identity := Current()
	if identity.GoVersion() != runtime.Version() {
		t.Fatalf("Current Go version = %q, want %q", identity.GoVersion(), runtime.Version())
	}
	if identity.Target().String() != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("Current target = %s, want %s/%s", identity.Target(), runtime.GOOS, runtime.GOARCH)
	}
}

func validBuildInfo() debug.BuildInfo {
	return debug.BuildInfo{
		Path:      MainPackagePath,
		GoVersion: "go1.26.5",
		Main:      debug.Module{Path: MainModulePath, Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: testRevision},
			{Key: "vcs.time", Value: "2026-07-01T02:03:04Z"},
			{Key: "vcs.modified", Value: "false"},
			{Key: "GOOS", Value: "linux"},
			{Key: "GOARCH", Value: "amd64"},
			{Key: "CGO_ENABLED", Value: "0"},
		},
	}
}

func validReleaseRequirement() ReleaseRequirement {
	requirement, err := NewReleaseRequirement("v1.2.3", testRevision, "go1.26.5")
	if err != nil {
		panic(err)
	}
	return requirement
}

func setBuildSetting(info *debug.BuildInfo, key string, value string) {
	for index := range info.Settings {
		if info.Settings[index].Key == key {
			info.Settings[index].Value = value
			return
		}
	}
	info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
}

func removeBuildSetting(info *debug.BuildInfo, key string) {
	filtered := info.Settings[:0]
	for _, setting := range info.Settings {
		if setting.Key != key {
			filtered = append(filtered, setting)
		}
	}
	info.Settings = filtered
}
