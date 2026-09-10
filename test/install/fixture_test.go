package install_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fixtureVersion  = "v1.2.3"
	fixtureNext     = "v1.2.4"
	fixtureRevision = "0123456789abcdef0123456789abcdef01234567"
	fixtureTime     = "2026-07-01T02:03:04Z"
	fixtureGo       = "go1.26.5"
)

type releaseFixture struct {
	version, revision, timestamp, toolchain, target string
	binary, archive, sidecar                        []byte
}

func fakeRelease(t *testing.T, version string) releaseFixture {
	t.Helper()
	target := "linux_amd64"
	if runtime.GOOS == "darwin" {
		target = "darwin_arm64"
	}
	release := releaseFixture{version: version, revision: fixtureRevision, timestamp: fixtureTime, toolchain: fixtureGo, target: target}
	goos, goarch, _ := strings.Cut(target, "_")
	identity := fmt.Sprintf(`{"schema_version":1,"version":%q,"revision":%q,"revision_time":%q,"source_state":"clean","vcs":"git","go_version":%q,"goos":%q,"goarch":%q}`, version, release.revision, release.timestamp, release.toolchain, goos, goarch)
	release.binary = []byte("#!/bin/sh\n[ \"$*\" = 'version --json' ] || exit 2\nprintf '%s\\n' '" + identity + "'\n")
	release.archive = executableArchive(t, release.binary, false)
	return release
}

func executableArchive(t *testing.T, binary []byte, extra bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for index, name := range []string{"daem", "extra"} {
		if index == 1 && !extra {
			break
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(binary); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func (release releaseFixture) archiveName() string {
	return "daem_" + strings.TrimPrefix(release.version, "v") + "_" + release.target + ".tar.gz"
}

type installerFixture struct {
	root, script, binDir, temporary string
	environment                     []string
	server                          *httptest.Server
	releases                        map[string]releaseFixture
	latest, fault                   string
	mu                              sync.Mutex
	requests                        []string
}

func newInstallerFixture(t *testing.T, fault string, releases ...releaseFixture) *installerFixture {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("installer requires an admitted Unix host")
	}
	script := os.Getenv("DAEM_TEST_INSTALL_SCRIPT")
	if script == "" {
		script = filepath.Join("..", "..", "install.sh")
	}
	script, err := filepath.Abs(script)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("installer entrypoint unavailable: %v", err)
	}
	if len(releases) == 0 {
		releases = []releaseFixture{fakeRelease(t, fixtureVersion), fakeRelease(t, fixtureNext)}
	}
	fixture := &installerFixture{root: t.TempDir(), script: script, fault: fault, releases: make(map[string]releaseFixture), latest: releases[0].version}
	for _, release := range releases {
		fixture.releases[release.version] = release
	}
	fixture.binDir = filepath.Join(fixture.root, "home", ".local", "bin")
	fixture.temporary = filepath.Join(fixture.root, "tmp")
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(fixture.server.Close)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	adapters := filepath.Join(fixture.root, "adapters")
	for _, directory := range []string{adapters, fixture.temporary, filepath.Join(fixture.root, "home"), filepath.Join(fixture.root, "config"), filepath.Join(fixture.root, "data"), filepath.Join(fixture.root, "cache"), filepath.Join(fixture.root, "state")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFixtureFile(t, filepath.Join(adapters, "curl"), []byte("#!/bin/sh\nexec "+shellQuote(executable)+" -- \"$@\"\n"), 0o700)
	writeFixtureFile(t, filepath.Join(adapters, "uname"), []byte("#!/bin/sh\ncase \"$1\" in -s) printf '%s\\n' \"$DAEM_TEST_SYSTEM\";; -m) printf '%s\\n' \"$DAEM_TEST_MACHINE\";; *) exit 2;; esac\n"), 0o700)
	writeFixtureFile(t, filepath.Join(adapters, "sysctl"), []byte("#!/bin/sh\nprintf '%s' \"$DAEM_TEST_TRANSLATED\"\n"), 0o700)
	writeFixtureFile(t, filepath.Join(adapters, "sw_vers"), []byte("#!/bin/sh\nprintf '%s' \"$DAEM_TEST_MACOS\"\nexit \"$DAEM_TEST_MACOS_EXIT\"\n"), 0o700)
	for _, relative := range []string{"home/.profile", "home/.bashrc", "config/daem.toml", "data/daem.lock", "state/recovery.json", "daem.toml", "daem.lock.toml", "observations/fixture"} {
		writeFixtureFile(t, filepath.Join(fixture.root, relative), []byte("do not change\n"), 0o600)
	}
	system, machine := "Linux", "x86_64"
	if releases[0].target == "darwin_arm64" {
		system, machine = "Darwin", "arm64"
	}
	fixture.environment = []string{
		"PATH=" + adapters + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + filepath.Join(fixture.root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(fixture.root, "config"),
		"XDG_DATA_HOME=" + filepath.Join(fixture.root, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(fixture.root, "cache"),
		"XDG_STATE_HOME=" + filepath.Join(fixture.root, "state"),
		"TMPDIR=" + fixture.temporary, "TMP=" + fixture.temporary, "TEMP=" + fixture.temporary,
		"LANG=C", "LC_ALL=C", "TERM=dumb",
		"DAEM_TEST_CURL_ENDPOINT=" + fixture.server.URL,
		"DAEM_TEST_SYSTEM=" + system, "DAEM_TEST_MACHINE=" + machine,
		"DAEM_TEST_TRANSLATED=0", "DAEM_TEST_MACOS=26.0\n", "DAEM_TEST_MACOS_EXIT=0",
	}
	return fixture
}

func (fixture *installerFixture) serve(w http.ResponseWriter, r *http.Request) {
	fixture.mu.Lock()
	fixture.requests = append(fixture.requests, r.Method+" "+r.URL.Path)
	fixture.mu.Unlock()
	if r.URL.Path == "/releases/latest" {
		location := fixture.server.URL + "/releases/tag/" + fixture.latest
		if fixture.fault == "latest prerelease" {
			location += "-rc.1"
		}
		http.Redirect(w, r, location, http.StatusFound)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/releases/tag/") {
		w.WriteHeader(http.StatusOK)
		return
	}
	for _, release := range fixture.releases {
		switch r.URL.Path {
		case "/api/commits/refs/tags/" + release.version:
			if r.Header.Get("Accept") != "application/vnd.github.sha" {
				http.Error(w, "wrong media type", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, release.revision)
			return
		case "/api/git/commits/" + release.revision:
			revision, timestamp := release.revision, release.timestamp
			if fixture.fault == "metadata sha" {
				revision = strings.Repeat("a", 40)
			}
			if fixture.fault == "committer time" {
				timestamp = "2000-01-01T00:00:00Z"
			}
			_, _ = fmt.Fprintf(w, `{"sha":%q,"author":{"date":"1999-01-01T00:00:00Z"},"committer":{"date":%q},"tree":{"sha":"different"}}`, revision, timestamp)
			return
		case "/raw/" + release.revision + "/go.mod":
			_, _ = fmt.Fprintf(w, "module github.com/isty2e/daem\ngo 1.25.0\ntoolchain %s\n", release.toolchain)
			if fixture.fault == "duplicate toolchain" {
				_, _ = fmt.Fprintf(w, "toolchain %s\n", release.toolchain)
			}
			return
		case "/releases/download/" + release.version + "/" + release.archiveName():
			if fixture.fault == "download" {
				http.Error(w, "fixture download failed", http.StatusServiceUnavailable)
				return
			}
			if fixture.fault == "interrupted download" {
				w.Header().Set("Content-Length", fmt.Sprint(len(release.archive)+100))
				_, _ = w.Write(release.archive[:len(release.archive)/2])
				return
			}
			_, _ = w.Write(release.archive)
			return
		case "/releases/download/" + release.version + "/" + release.archiveName() + ".sha256":
			if release.sidecar != nil {
				_, _ = w.Write(release.sidecar)
				return
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(release.archive))
			name := release.archiveName()
			if fixture.fault == "checksum" {
				digest = strings.Repeat("0", 64)
			}
			if fixture.fault == "checksum name" {
				name = "other.tar.gz"
			}
			_, _ = fmt.Fprintf(w, "%s  %s\n", digest, name)
			return
		}
	}
	http.NotFound(w, r)
}

func (fixture *installerFixture) run(t *testing.T, wantSuccess bool, args ...string) string {
	t.Helper()
	before := fixture.protectedTree(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", append([]string{fixture.script}, args...)...)
	command.Dir = fixture.root
	command.Env = fixture.environment
	output, err := command.CombinedOutput()
	if (err == nil) != wantSuccess {
		t.Fatalf("installer %v: error=%v want success=%t\n%s", args, err, wantSuccess, output)
	}
	if ctx.Err() != nil {
		t.Fatalf("installer exceeded process deadline: %v\n%s", ctx.Err(), output)
	}
	after := fixture.protectedTree(t)
	for path, entry := range after {
		if _, existed := before[path]; !existed && entry.mode.IsDir() && pathWithin(fixture.binDir, path) {
			delete(after, path)
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("installer changed files outside its destination: before=%v after=%v", before, after)
	}
	entries, err := os.ReadDir(fixture.temporary)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary staging left behind: %v, %v", entries, err)
	}
	entries, err = os.ReadDir(fixture.binDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".daem-install.") {
			t.Fatalf("destination staging left behind: %s", entry.Name())
		}
	}
	return string(output)
}

type fixtureEntry struct {
	mode   os.FileMode
	digest [sha256.Size]byte
	target string
}

func pathWithin(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (fixture *installerFixture) protectedTree(t *testing.T) map[string]fixtureEntry {
	t.Helper()
	entries := make(map[string]fixtureEntry)
	err := filepath.WalkDir(fixture.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, excluded := range []string{fixture.binDir, fixture.temporary, filepath.Join(fixture.root, "observations")} {
			if pathWithin(path, excluded) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		observation := fixtureEntry{mode: info.Mode()}
		if info.Mode().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			observation.digest = sha256.Sum256(contents)
		} else if info.Mode()&os.ModeSymlink != 0 {
			observation.target, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		entries[path] = observation
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func (fixture *installerFixture) requestCount(prefix string) int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	count := 0
	for _, request := range fixture.requests {
		if strings.HasPrefix(request, prefix) {
			count++
		}
	}
	return count
}

func (fixture *installerFixture) setEnvironment(key, value string) {
	for index, entry := range fixture.environment {
		if strings.HasPrefix(entry, key+"=") {
			fixture.environment[index] = key + "=" + value
			return
		}
	}
	fixture.environment = append(fixture.environment, key+"="+value)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func writeFixtureFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("file %s: read error=%v; got %d bytes sha256=%x, want %d bytes sha256=%x", path, err, len(got), sha256.Sum256(got), len(want), sha256.Sum256(want))
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s absent; error=%v", path, err)
	}
}

func TestMain(m *testing.M) {
	if endpoint := os.Getenv("DAEM_TEST_CURL_ENDPOINT"); endpoint != "" {
		os.Exit(forwardFixtureCurl(endpoint))
	}
	os.Exit(m.Run())
}

func forwardFixtureCurl(endpoint string) int {
	index := slices.Index(os.Args, "--")
	if index < 0 || !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		return 64
	}
	args, err := fixtureCurlArguments(endpoint, os.Args[index+1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 64
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/curl", args...)
	command.Stderr = os.Stderr
	output, err := command.Output()
	_, _ = os.Stdout.Write(bytes.ReplaceAll(output, []byte(endpoint+"/releases/"), []byte("https://github.com/isty2e/daem/releases/")))
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func fixtureCurlArguments(endpoint string, original []string) ([]string, error) {
	args := slices.Clone(original)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--fail", "--silent", "--show-error", "--location", "--head":
			continue
		case "--connect-timeout", "--max-time", "--max-redirs", "--max-filesize", "--output", "--header", "--write-out":
			index++
			if index >= len(args) {
				return nil, fmt.Errorf("fixture curl option %s needs a value", arg)
			}
			continue
		}
		if index != len(args)-1 {
			return nil, fmt.Errorf("fixture refused unexpected curl argument %q", arg)
		}
		switch {
		case strings.HasPrefix(arg, "https://github.com/isty2e/daem/releases/"):
			args[index] = endpoint + strings.TrimPrefix(arg, "https://github.com/isty2e/daem")
		case strings.HasPrefix(arg, "https://api.github.com/repos/isty2e/daem/"):
			args[index] = endpoint + "/api/" + strings.TrimPrefix(arg, "https://api.github.com/repos/isty2e/daem/")
		case strings.HasPrefix(arg, "https://raw.githubusercontent.com/isty2e/daem/"):
			args[index] = endpoint + "/raw/" + strings.TrimPrefix(arg, "https://raw.githubusercontent.com/isty2e/daem/")
		default:
			return nil, fmt.Errorf("fixture refused unexpected URL %q", arg)
		}
		return args, nil
	}
	return nil, fmt.Errorf("fixture curl request needs one mapped URL")
}

func TestFixtureCurlRejectsUnmappedRequestsBeforeExecution(t *testing.T) {
	const endpoint = "http://127.0.0.1:12345"
	const url = "https://github.com/isty2e/daem/releases/latest"
	args, err := fixtureCurlArguments(endpoint, []string{"--head", "--output", "/dev/null", url})
	if err != nil || !slices.Equal(args, []string{"--head", "--output", "/dev/null", endpoint + "/releases/latest"}) {
		t.Fatalf("mapped curl arguments = %v, error=%v", args, err)
	}
	for _, args := range [][]string{
		{"github.com/isty2e/daem/releases/latest"},
		{"https://example.invalid/asset"},
		{"another-host.invalid", url},
		{url, url},
		{"--config", "arbitrary-config", url},
		{"--header"},
		{"--head"},
	} {
		if _, err := fixtureCurlArguments(endpoint, args); err == nil {
			t.Errorf("accepted unmapped curl arguments: %v", args)
		}
	}
}
