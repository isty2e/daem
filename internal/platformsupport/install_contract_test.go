package platformsupport

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	goversion "go/version"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

func TestInstallRecipeUsesExecutableMacOSRuntimeFloor(t *testing.T) {
	document := installRecipeDocument(t)
	for _, required := range []string{
		`daem_admitted_macos_product_version() {`,
		`/usr/bin/sw_vers --productVersion > "$DAEM_MACOS_VERSION_FILE"`,
		`daem_admitted_macos_product_version < "$DAEM_MACOS_VERSION_FILE" > /dev/null`,
		`requires macOS 26.0 or newer`,
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("docs/install.md is missing runtime contract %q", required)
		}
	}
	if strings.Contains(document, `DAEM_MACOS_VERSION="$(/usr/bin/sw_vers --productVersion)"`) {
		t.Fatal("docs/install.md captures raw sw_vers output through command substitution")
	}

	tests := []struct {
		version   string
		supported bool
	}{
		{version: ""},
		{version: "25.9.9"},
		{version: "26"},
		{version: "026.0"},
		{version: "26.00"},
		{version: "26.x"},
		{version: "26.0 "},
		{version: "26.0\n27.0"},
		{version: "26.0.0.0"},
		{version: "4294967296.0"},
		{version: "26.4294967296"},
		{version: "26.0", supported: true},
		{version: "26.5.1", supported: true},
		{version: "27.0", supported: true},
		{version: "4294967295.0", supported: true},
	}
	functions := installRecipeFunctions(t)
	for _, test := range tests {
		err := runInstallShell(
			functions,
			`printf '%s' "$1" | daem_admitted_macos_product_version >/dev/null`,
			nil,
			test.version,
		)
		if (err == nil) != test.supported {
			t.Fatalf("installer macOS %q error = %v, want supported=%t", test.version, err, test.supported)
		}
	}
}

func TestInstallRecipePreservesRawMacOSProductVersionOutput(t *testing.T) {
	functions := installRecipeFunctions(t)
	fixtureRoot := t.TempDir()
	fixturePath := filepath.Join(fixtureRoot, "sw-vers-fixture")
	if err := os.WriteFile(fixturePath, []byte("#!/bin/sh\nprintf '%s' \"$DAEM_TEST_SW_VERS_OUTPUT\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		output    string
		canonical string
	}{
		{name: "no trailing newline", output: "26.0", canonical: "26.0"},
		{name: "one trailing newline", output: "26.0\n", canonical: "26.0"},
		{name: "two trailing newlines", output: "26.0\n\n"},
		{name: "multiple lines", output: "26.0\n27.0\n"},
		{name: "carriage return", output: "26.0\r\n"},
		{name: "below floor", output: "25.9.9\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capturePath := filepath.Join(t.TempDir(), "macos-product-version")
			script := `
if ! "$1" > "$2"; then
  exit 1
fi
DAEM_MACOS_VERSION="$(daem_admitted_macos_product_version < "$2")" || exit 1
test "$DAEM_MACOS_VERSION" = "$3"
`
			err := runInstallShell(
				functions,
				script,
				[]string{"DAEM_TEST_SW_VERS_OUTPUT=" + test.output},
				fixturePath,
				capturePath,
				test.canonical,
			)
			if (err == nil) != (test.canonical != "") {
				t.Fatalf("raw output %q error = %v, want canonical %q", test.output, err, test.canonical)
			}
		})
	}
}

func TestInstallRecipeSelectsNativeReleaseTarget(t *testing.T) {
	functions := installRecipeFunctions(t)
	tests := []struct {
		name       string
		system     string
		machine    string
		translated string
		target     string
	}{
		{name: "native Apple silicon", system: "Darwin", machine: "arm64", target: "darwin_arm64"},
		{name: "Rosetta Apple silicon", system: "Darwin", machine: "x86_64", translated: "1", target: "darwin_arm64"},
		{name: "native Linux x86-64", system: "Linux", machine: "x86_64", target: "linux_amd64"},
		{name: "Intel Mac", system: "Darwin", machine: "x86_64", translated: "0"},
		{name: "missing Rosetta evidence", system: "Darwin", machine: "x86_64"},
		{name: "malformed Rosetta evidence", system: "Darwin", machine: "x86_64", translated: "01"},
		{name: "unsupported Linux architecture", system: "Linux", machine: "arm64"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runInstallShell(
				functions,
				`actual="$(daem_release_target "$1" "$2" "$3")" || exit 1
test "$actual" = "$4"`,
				nil,
				test.system,
				test.machine,
				test.translated,
				test.target,
			)
			if (err == nil) != (test.target != "") {
				t.Fatalf("target selection error = %v, want target %q", err, test.target)
			}
		})
	}
}

func TestInstallRecipeRejectsUnsafeReleaseVersionTokens(t *testing.T) {
	functions := installRecipeFunctions(t)
	tests := []struct {
		version  string
		admitted bool
	}{
		{version: "v0.1.0", admitted: true},
		{version: "v1.2.3-rc.1", admitted: true},
		{version: "v1.2.3-0", admitted: true},
		{version: "v1.2.3-alpha-1", admitted: true},
		{version: "v1.2.3-rc.0.20260701020304", admitted: true},
		{version: "v1.2.3-rc0.20260701020304-0123456789ab", admitted: true},
		{version: ""},
		{version: "1.2.3"},
		{version: "v1.2"},
		{version: "v01.2.3"},
		{version: "v1.02.3"},
		{version: "v1.2.03"},
		{version: "v1.2.3-"},
		{version: "v1.2.3-01"},
		{version: "v1.2.3-alpha..1"},
		{version: "v1.2.3-alpha_1"},
		{version: "v1.2.3+build"},
		{version: "v0.0.0-20260701020304-0123456789ab"},
		{version: "v1.2.4-0.20260701020304-0123456789ab"},
		{version: "v1.2.3-rc.0.20260701020304-0123456789ab"},
		{version: "v1.2.3-rc0.0.20260701020304-0123456789ab"},
		{version: "v1.2.3/../../other"},
		{version: "v1.2.3 other"},
		{version: "v1.2.3\nv2.0.0"},
	}
	for _, test := range tests {
		producerAdmitted := semver.IsValid(test.version) &&
			semver.Build(test.version) == "" &&
			semver.Canonical(test.version) == test.version &&
			!module.IsPseudoVersion(test.version)
		if test.admitted != producerAdmitted {
			t.Fatalf("version test case %q admitted=%t, producer contract=%t", test.version, test.admitted, producerAdmitted)
		}
		err := runInstallShell(
			functions,
			`daem_admitted_release_version_token "$1"`,
			nil,
			test.version,
		)
		if (err == nil) != test.admitted {
			t.Fatalf("version token %q error = %v, want admitted=%t", test.version, err, test.admitted)
		}
	}
}

func TestInstallRecipeTimestampMatchesCanonicalBuildIdentity(t *testing.T) {
	functions := installRecipeFunctions(t)
	timestamps := []string{
		"2026-07-01T02:03:04Z",
		"2026-07-01T02:03:04.1Z",
		"2026-07-01T02:03:04.123456789Z",
		"2024-02-29T23:59:59Z",
		"",
		"T",
		"----",
		"2026-99-99T99:99:99Z",
		"2023-02-29T02:03:04Z",
		"2026-07-01T02:03:60Z",
		"2026-07-01T02:03:04+00:00",
		"2026-07-01T02:03:04.10Z",
		"2026-07-01T02:03:04.1234567890Z",
	}
	for _, timestamp := range timestamps {
		parsed, parseErr := time.Parse(time.RFC3339, timestamp)
		_, offset := parsed.Zone()
		producerCanonical := parseErr == nil && offset == 0 && parsed.UTC().Format(time.RFC3339Nano) == timestamp
		err := runInstallShell(functions, `daem_admitted_release_timestamp "$1"`, nil, timestamp)
		if (err == nil) != producerCanonical {
			t.Fatalf("timestamp %q error=%v, producer canonical=%t", timestamp, err, producerCanonical)
		}
	}
}

func TestInstallRecipeAdmitsStableReleaseToolchainVersions(t *testing.T) {
	functions := installRecipeFunctions(t)
	tests := []struct {
		version  string
		admitted bool
	}{
		{version: "go1.26.5", admitted: true},
		{version: "go2.0.0", admitted: true},
		{version: "go1evil"},
		{version: "go1.999"},
		{version: "go01.26.5"},
		{version: "go1.026.5"},
		{version: "go1.26.05"},
		{version: "go1.26rc1"},
		{version: "devel go1.27"},
	}
	for _, test := range tests {
		if test.admitted && !goversion.IsValid(test.version) {
			t.Fatalf("admitted toolchain %q is invalid under the producer parser", test.version)
		}
		err := runInstallShell(functions, `daem_admitted_release_go_version "$1"`, nil, test.version)
		if (err == nil) != test.admitted {
			t.Fatalf("toolchain %q error=%v, want admitted=%t", test.version, err, test.admitted)
		}
	}
}

func TestInstallRecipeRequiresExactArchiveChecksumEntry(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("checksum tools are defined only for supported installer systems")
	}
	functions := installRecipeFunctions(t)
	root := filepath.Join(t.TempDir(), `stage\with-backslash`)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	archiveName := "daem_1.2.3_linux_amd64.tar.gz"
	archivePath := filepath.Join(root, archiveName)
	payload := []byte("archive payload")
	if err := os.WriteFile(archivePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	tests := []struct {
		name     string
		sidecar  string
		verified bool
	}{
		{name: "exact entry", sidecar: digest + "  " + archiveName + "\n", verified: true},
		{name: "wrong filename", sidecar: digest + "  other.tar.gz\n"},
		{name: "wrong digest", sidecar: strings.Repeat("0", 64) + "  " + archiveName + "\n"},
		{name: "uppercase digest", sidecar: strings.ToUpper(digest) + "  " + archiveName + "\n"},
		{name: "additional entry", sidecar: digest + "  " + archiveName + "\n" + digest + "  other.tar.gz\n"},
		{name: "trailing blank entry", sidecar: digest + "  " + archiveName + "\n\n"},
		{name: "additional field", sidecar: digest + "  " + archiveName + " extra\n"},
		{name: "empty sidecar"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sidecarPath := filepath.Join(t.TempDir(), archiveName+".sha256")
			if err := os.WriteFile(sidecarPath, []byte(test.sidecar), 0o600); err != nil {
				t.Fatal(err)
			}
			err := runInstallShell(
				functions,
				`daem_verify_archive_checksum "$1" "$2" "$3" "$4"`,
				nil,
				archivePath,
				sidecarPath,
				archiveName,
				platformSystemName(runtime.GOOS),
			)
			if (err == nil) != test.verified {
				t.Fatalf("checksum verification error = %v, want verified=%t", err, test.verified)
			}
		})
	}
	t.Run("checksum command failure", func(t *testing.T) {
		sidecarPath := filepath.Join(t.TempDir(), archiveName+".sha256")
		if err := os.WriteFile(sidecarPath, []byte(digest+"  "+archiveName+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := runInstallShell(
			functions,
			`daem_verify_archive_checksum "$1" "$2" "$3" "$4"`,
			nil,
			filepath.Join(root, "missing.tar.gz"),
			sidecarPath,
			archiveName,
			platformSystemName(runtime.GOOS),
		)
		if err == nil {
			t.Fatal("checksum verification ignored a checksum command failure")
		}
	})
}

func TestInstallRecipeRequiresOneRegularExecutableArchiveEntry(t *testing.T) {
	functions := installRecipeFunctions(t)
	tests := []struct {
		name      string
		entries   []installArchiveEntry
		extracted bool
	}{
		{
			name:      "exact executable",
			entries:   []installArchiveEntry{{name: "daem", mode: 0o755, body: []byte("binary")}},
			extracted: true,
		},
		{name: "wrong name", entries: []installArchiveEntry{{name: "other", mode: 0o755, body: []byte("binary")}}},
		{
			name: "additional entry",
			entries: []installArchiveEntry{
				{name: "daem", mode: 0o755, body: []byte("binary")},
				{name: "README", mode: 0o644, body: []byte("extra")},
			},
		},
		{name: "non-executable", entries: []installArchiveEntry{{name: "daem", mode: 0o644, body: []byte("binary")}}},
		{name: "symlink", entries: []installArchiveEntry{{name: "daem", mode: 0o755, typeflag: tar.TypeSymlink, linkname: "missing"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archivePath := filepath.Join(root, "release.tar.gz")
			writeInstallArchive(t, archivePath, test.entries)
			err := runInstallShell(
				functions,
				`daem_extract_release_binary "$1" "$2" "$3"`,
				nil,
				archivePath,
				filepath.Join(root, "extracted"),
				filepath.Join(root, "entries"),
			)
			if (err == nil) != test.extracted {
				t.Fatalf("archive extraction error = %v, want extracted=%t", err, test.extracted)
			}
		})
	}
}

func TestInstallRecipeRequiresExactReleaseBinaryIdentity(t *testing.T) {
	functions := installRecipeFunctions(t)
	const (
		expectedRevision     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		expectedRevisionTime = "2026-07-01T02:03:04Z"
		expectedGoVersion    = "go1.26.5"
	)
	valid := `{
  "schema_version": 1,
  "version": "v1.2.3",
  "revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "revision_time": "2026-07-01T02:03:04Z",
  "source_state": "clean",
  "vcs": "git",
  "go_version": "go1.26.5",
  "goos": "linux",
  "goarch": "amd64"
}
`
	tests := []struct {
		name         string
		content      string
		version      string
		revision     string
		revisionTime string
		goVersion    string
		target       string
		verified     bool
	}{
		{name: "exact identity", content: valid, version: "v1.2.3", target: "linux_amd64", verified: true},
		{
			name:     "compact reordered identity",
			content:  `{"goarch":"amd64","goos":"linux","go_version":"go1.26.5","vcs":"git","source_state":"clean","revision_time":"2026-07-01T02:03:04Z","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","version":"v1.2.3","schema_version":1}`,
			version:  "v1.2.3",
			target:   "linux_amd64",
			verified: true,
		},
		{name: "wrong version", content: valid, version: "v1.2.4", target: "linux_amd64"},
		{name: "wrong revision", content: valid, version: "v1.2.3", revision: strings.Repeat("b", 40), target: "linux_amd64"},
		{name: "wrong revision time", content: valid, version: "v1.2.3", revisionTime: "2026-07-01T02:03:05Z", target: "linux_amd64"},
		{name: "wrong Go version", content: valid, version: "v1.2.3", goVersion: "go1.25.12", target: "linux_amd64"},
		{name: "whitespace inside version", content: strings.Replace(valid, `"version": "v1.2.3"`, `"version": "v1. 2.3"`, 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "escaped version", content: strings.Replace(valid, `"version": "v1.2.3"`, `"version": "v1.2.3\u002drc"`, 1), version: "v1.2.3-rc", target: "linux_amd64"},
		{name: "wrong target", content: valid, version: "v1.2.3", target: "darwin_arm64"},
		{name: "modified source", content: strings.Replace(valid, `"source_state": "clean"`, `"source_state": "modified"`, 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "unknown vcs", content: strings.Replace(valid, `"vcs": "git"`, `"vcs": "unknown"`, 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "short revision", content: strings.Replace(valid, strings.Repeat("a", 40), strings.Repeat("a", 39), 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "unknown revision time", content: strings.Replace(valid, `"revision_time": "2026-07-01T02:03:04Z"`, `"revision_time": "unknown"`, 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "malformed revision time token", content: strings.Replace(valid, expectedRevisionTime, "T", 1), version: "v1.2.3", revisionTime: "T", target: "linux_amd64"},
		{name: "malformed revision time punctuation", content: strings.Replace(valid, expectedRevisionTime, "----", 1), version: "v1.2.3", revisionTime: "----", target: "linux_amd64"},
		{name: "invalid revision time fields", content: strings.Replace(valid, expectedRevisionTime, "2026-99-99T99:99:99Z", 1), version: "v1.2.3", revisionTime: "2026-99-99T99:99:99Z", target: "linux_amd64"},
		{name: "unknown Go version", content: strings.Replace(valid, `"go_version": "go1.26.5"`, `"go_version": "unknown"`, 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "malformed Go version", content: strings.Replace(valid, expectedGoVersion, "go1evil", 1), version: "v1.2.3", goVersion: "go1evil", target: "linux_amd64"},
		{name: "different Go release", content: strings.Replace(valid, expectedGoVersion, "go1.999", 1), version: "v1.2.3", goVersion: "go1.999", target: "linux_amd64"},
		{name: "pseudo release version", content: strings.Replace(valid, "v1.2.3", "v0.0.0-20260701020304-0123456789ab", 1), version: "v0.0.0-20260701020304-0123456789ab", target: "linux_amd64"},
		{name: "future schema", content: strings.Replace(valid, `"schema_version": 1`, `"schema_version": 2`, 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "duplicate version", content: strings.Replace(valid, `  "version": "v1.2.3",`, "  \"version\": \"v1.2.3\",\n  \"version\": \"v1.2.3\",", 1), version: "v1.2.3", target: "linux_amd64"},
		{name: "additional field", content: strings.Replace(valid, `  "goarch": "amd64"`, "  \"goarch\": \"amd64\",\n  \"unexpected\": true", 1), version: "v1.2.3", target: "linux_amd64"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			revision := test.revision
			if revision == "" {
				revision = expectedRevision
			}
			revisionTime := test.revisionTime
			if revisionTime == "" {
				revisionTime = expectedRevisionTime
			}
			goVersion := test.goVersion
			if goVersion == "" {
				goVersion = expectedGoVersion
			}
			identityPath := filepath.Join(t.TempDir(), "version.json")
			if err := os.WriteFile(identityPath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := runInstallShell(
				functions,
				`daem_release_binary_matches "$1" "$2" "$3" "$4" "$5" "$6"`,
				nil,
				identityPath,
				test.version,
				revision,
				revisionTime,
				goVersion,
				test.target,
			)
			if (err == nil) != test.verified {
				t.Fatalf("binary identity verification error = %v, want verified=%t", err, test.verified)
			}
		})
	}
}

func TestInstallRecipeUsesExactPublishedReleaseRequirement(t *testing.T) {
	recipe := installRecipe(t)
	for _, fact := range []string{
		"DAEM_VERSION=v0.1.0",
		"DAEM_REVISION_TIME=2026-07-28T02:19:30Z",
		"DAEM_GO_VERSION=go1.26.5",
		`DAEM_ORIGIN_URL="https://github.com/isty2e/daem.git"`,
		`DAEM_REVISION="$(git ls-remote "$DAEM_ORIGIN_URL" "refs/tags/${DAEM_VERSION}^{}" | cut -f1)"`,
		`DAEM_REVISION="$(git ls-remote "$DAEM_ORIGIN_URL" "refs/tags/${DAEM_VERSION}" | cut -f1)"`,
	} {
		if !strings.Contains(recipe, fact) {
			t.Fatalf("install recipe is missing exact published release fact %q", fact)
		}
	}
	if literalRevision := regexp.MustCompile(`(?m)^DAEM_REVISION=[0-9a-f]{40}$`); literalRevision.MatchString(recipe) {
		t.Fatal("install recipe pins a literal release revision that cannot equal its own tag")
	}
	if !goversion.IsValid("go1.26.5") {
		t.Fatal("documented release toolchain is not a valid Go version")
	}
	revisionTime, err := time.Parse(time.RFC3339, "2026-07-28T02:19:30Z")
	if err != nil || revisionTime.Format(time.RFC3339Nano) != "2026-07-28T02:19:30Z" {
		t.Fatalf("documented revision time is not canonical: %v", err)
	}
}

func TestInstallRecipeVerifiesBeforeReplacingExecutable(t *testing.T) {
	recipe := installRecipe(t)
	assertInstallRecipeOrder(t, recipe, `daem_admitted_release_requirement`, `curl --fail --location`)
	assertInstallRecipeOrder(t, recipe, `daem_release_target "$DAEM_SYSTEM" "$DAEM_MACHINE" "$DAEM_TRANSLATED"`, `curl --fail --location`)
	assertInstallRecipeOrder(t, recipe, `daem_verify_archive_checksum`, `daem_extract_release_binary`)
	assertInstallRecipeOrder(t, recipe, `daem_extract_release_binary`, `daem_release_binary_matches`)
	assertInstallRecipeOrder(t, recipe, `daem_release_binary_matches`, `DAEM_BIN="$HOME/.local/bin/daem"`)
}

func TestInstallRecipeHasValidShellSyntax(t *testing.T) {
	command := exec.Command("/bin/sh", "-n")
	command.Stdin = strings.NewReader(installRecipe(t))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install recipe shell syntax error: %v\n%s", err, output)
	}
}

func TestMacOSRuntimeFloorMatchesPublicContracts(t *testing.T) {
	admission, err := Lookup("darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	minimum, required := admission.RuntimeRequirement()
	if !required || minimum.String() != "26.0" {
		t.Fatalf("Darwin runtime requirement = %s,%t", minimum, required)
	}
	for _, path := range []string{"README.md", "docs/install.md", "docs/platforms.md"} {
		content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "macOS 26") {
			t.Fatalf("%s does not disclose macOS 26 floor", path)
		}
	}
}

type installArchiveEntry struct {
	name     string
	mode     int64
	typeflag byte
	linkname string
	body     []byte
}

func installRecipeDocument(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "docs", "install.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func installRecipe(t *testing.T) string {
	t.Helper()
	document := installRecipeDocument(t)
	const opening = "```bash\n"
	start := strings.Index(document, opening)
	if start < 0 {
		t.Fatal("docs/install.md has no bash install recipe")
	}
	start += len(opening)
	end := strings.Index(document[start:], "\n```")
	if end < 0 {
		t.Fatal("docs/install.md has an unterminated bash install recipe")
	}
	return document[start : start+end]
}

func installRecipeFunctions(t *testing.T) string {
	t.Helper()
	recipe := installRecipe(t)
	end := strings.Index(recipe, "\nDAEM_VERSION=")
	if end < 0 {
		t.Fatal("docs/install.md install recipe does not separate functions from execution")
	}
	return recipe[:end]
}

func runInstallShell(functions string, invocation string, environment []string, arguments ...string) error {
	commandArguments := []string{"-c", functions + "\n" + invocation, "daem-install-test"}
	commandArguments = append(commandArguments, arguments...)
	command := exec.Command("/bin/sh", commandArguments...)
	command.Env = append(os.Environ(), environment...)
	return command.Run()
}

func platformSystemName(goos string) string {
	if goos == "darwin" {
		return "Darwin"
	}
	return "Linux"
}

func writeInstallArchive(t *testing.T, path string, entries []installArchiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Size:     int64(len(entry.body)),
			Typeflag: typeflag,
			Linkname: entry.linkname,
			Format:   tar.FormatUSTAR,
		}
		if typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if typeflag == tar.TypeReg {
			if _, err := archive.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertInstallRecipeOrder(t *testing.T, recipe string, before string, after string) {
	t.Helper()
	beforeIndex := strings.LastIndex(recipe, before)
	afterIndex := strings.LastIndex(recipe, after)
	if beforeIndex < 0 || afterIndex < 0 || beforeIndex >= afterIndex {
		t.Fatalf("install recipe order %q before %q is not enforced", before, after)
	}
}
