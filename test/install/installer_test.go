package install_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallerInstallUpgradeRepeatAndOfflineRollback(t *testing.T) {
	fixture := newInstallerFixture(t, "")
	current := filepath.Join(fixture.binDir, "daem")
	previous := current + ".previous"
	fixture.run(t, true, "--version", fixtureVersion)
	assertFile(t, current, fixture.releases[fixtureVersion].binary)
	assertAbsent(t, previous)
	if fixture.requestCount("HEAD /releases/latest") != 0 {
		t.Fatal("explicit install discovered latest")
	}
	fixture.run(t, true, "--version", fixtureNext)
	assertFile(t, current, fixture.releases[fixtureNext].binary)
	assertFile(t, previous, fixture.releases[fixtureVersion].binary)
	before, err := os.Stat(previous)
	if err != nil {
		t.Fatal(err)
	}
	fixture.run(t, true, "--version", fixtureNext)
	assertFile(t, previous, fixture.releases[fixtureVersion].binary)
	after, err := os.Stat(previous)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("same-byte reinstall replaced previous: %v", err)
	}
	fixture.server.Close()
	poison := filepath.Join(fixture.root, "observations", "offline-network-attempt")
	writeFixtureFile(t, filepath.Join(fixture.root, "adapters", "curl"), []byte("#!/bin/sh\nprintf attempted > "+shellQuote(poison)+"\nexit 99\n"), 0o700)
	fixture.run(t, true, "--rollback")
	assertFile(t, current, fixture.releases[fixtureVersion].binary)
	assertFile(t, previous, fixture.releases[fixtureVersion].binary)
	assertAbsent(t, poison)
}

func TestInstallerLatestBindsOnceAndSupportsSpacedDirectory(t *testing.T) {
	fixture := newInstallerFixture(t, "")
	fixture.binDir = filepath.Join(fixture.root, "custom executable directory")
	fixture.run(t, true, "--bin-dir", fixture.binDir)
	assertFile(t, filepath.Join(fixture.binDir, "daem"), fixture.releases[fixtureVersion].binary)
	if fixture.requestCount("HEAD /releases/latest") != 1 || fixture.requestCount("GET /api/commits/refs/tags/"+fixtureVersion) != 1 {
		t.Fatalf("release was not bound once: %v", fixture.requests)
	}
	if fixture.requestCount("GET /releases/download/"+fixtureVersion+"/") != 2 {
		t.Fatalf("archive and checksum were not selected by exact tag: %v", fixture.requests)
	}
	assertAbsent(t, filepath.Join(fixture.root, "home", ".local", "bin"))
}

func TestInstallerHelpNeedsNeitherHomeNorExternalTools(t *testing.T) {
	fixture := newInstallerFixture(t, "")
	fixture.setEnvironment("HOME", "")
	fixture.setEnvironment("PATH", "")
	output := fixture.run(t, true, "--help")
	if !strings.Contains(output, "Usage: sh "+filepath.Base(fixture.script)) || fixture.requestCount("") != 0 {
		t.Fatalf("help output=%q, requests=%v", output, fixture.requests)
	}
	assertAbsent(t, fixture.binDir)
}

func TestInstallerArgumentsAndHelpHaveNoInstallationEffects(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		success bool
	}{
		{"help on unsupported platform", []string{"--help"}, true},
		{"duplicate help", []string{"--help", "--help"}, false},
		{"help with unknown flag", []string{"--help", "--unknown"}, false},
		{"destination followed by help", []string{"--bin-dir", "--help"}, false},
		{"unknown", []string{"--unknown"}, false},
		{"missing version", []string{"--version"}, false},
		{"invalid version", []string{"--version", "latest"}, false},
		{"empty version", []string{"--version", ""}, false},
		{"duplicate version", []string{"--version", fixtureVersion, "--version", fixtureVersion}, false},
		{"missing destination", []string{"--bin-dir"}, false},
		{"empty destination", []string{"--bin-dir", ""}, false},
		{"duplicate destination", []string{"--bin-dir", "a", "--bin-dir", "b"}, false},
		{"duplicate rollback", []string{"--rollback", "--rollback"}, false},
		{"conflicting rollback", []string{"--rollback", "--version", fixtureVersion}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInstallerFixture(t, "")
			fixture.setEnvironment("DAEM_TEST_SYSTEM", "Windows")
			fixture.run(t, test.success, test.args...)
			if fixture.requestCount("") != 0 {
				t.Fatalf("argument handling performed requests: %v", fixture.requests)
			}
			assertAbsent(t, fixture.binDir)
		})
	}
}

func TestInstallerRejectsUnsupportedRuntimeBeforeNetwork(t *testing.T) {
	tests := []struct {
		name, system, machine, translated, macos, exit string
	}{
		{"unsupported OS", "FreeBSD", "x86_64", "", "", "0"},
		{"unsupported Linux arch", "Linux", "aarch64", "", "", "0"},
		{"Intel Mac", "Darwin", "x86_64", "0", "26.0\n", "0"},
		{"missing translation evidence", "Darwin", "x86_64", "", "26.0\n", "0"},
		{"old macOS", "Darwin", "arm64", "", "25.9\n", "0"},
		{"malformed macOS", "Darwin", "arm64", "", "26\n", "0"},
		{"extra newline", "Darwin", "arm64", "", "26.0\n\n", "0"},
		{"failed observation with valid output", "Darwin", "arm64", "", "26.0\n", "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInstallerFixture(t, "")
			fixture.setEnvironment("DAEM_TEST_SYSTEM", test.system)
			fixture.setEnvironment("DAEM_TEST_MACHINE", test.machine)
			fixture.setEnvironment("DAEM_TEST_TRANSLATED", test.translated)
			fixture.setEnvironment("DAEM_TEST_MACOS", test.macos)
			fixture.setEnvironment("DAEM_TEST_MACOS_EXIT", test.exit)
			fixture.run(t, false, "--version", fixtureVersion)
			if fixture.requestCount("") != 0 {
				t.Fatalf("unsupported runtime made requests: %v", fixture.requests)
			}
			assertAbsent(t, fixture.binDir)
		})
	}
}

func TestInstallerTranslatedAppleSiliconSelectsNativeArtifact(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Apple silicon fixture uses the native Darwin checksum tool")
	}
	fixture := newInstallerFixture(t, "")
	fixture.setEnvironment("DAEM_TEST_MACHINE", "x86_64")
	fixture.setEnvironment("DAEM_TEST_TRANSLATED", "1")
	fixture.run(t, true, "--version", fixtureVersion)
	assertFile(t, filepath.Join(fixture.binDir, "daem"), fixture.releases[fixtureVersion].binary)
}

func TestInstallerAcquisitionFailuresPreserveExecutables(t *testing.T) {
	tests := []struct{ fault, diagnostic string }{
		{"metadata sha", "release commit metadata"},
		{"committer time", "requested release identity"},
		{"duplicate toolchain", "toolchain directive"},
		{"download", "requested release archive"},
		{"interrupted download", "requested release archive"},
		{"checksum", "exact checksum entry"},
		{"checksum name", "exact checksum entry"},
		{"latest prerelease", "latest stable release"},
		{"wrong identity", "requested release identity"},
		{"extra archive entry", "one regular executable"},
	}
	for _, test := range tests {
		t.Run(test.fault, func(t *testing.T) {
			release := fakeRelease(t, fixtureVersion)
			if test.fault == "wrong identity" {
				release.binary = bytes.ReplaceAll(release.binary, []byte(fixtureVersion), []byte("v9.9.9"))
				release.archive = executableArchive(t, release.binary, false)
			}
			if test.fault == "extra archive entry" {
				release.archive = executableArchive(t, release.binary, true)
			}
			fixture := newInstallerFixture(t, test.fault, release)
			current := filepath.Join(fixture.binDir, "daem")
			old, previous := []byte("retained current\n"), []byte("retained previous\n")
			writeFixtureFile(t, current, old, 0o755)
			writeFixtureFile(t, current+".previous", previous, 0o755)
			args := []string{"--version", fixtureVersion}
			if test.fault == "latest prerelease" {
				args = nil
			}
			output := fixture.run(t, false, args...)
			if !strings.Contains(output, test.diagnostic) {
				t.Fatalf("wrong failure, want %q:\n%s", test.diagnostic, output)
			}
			assertFile(t, current, old)
			assertFile(t, current+".previous", previous)
		})
	}
}

func TestInstallerOrdinaryPreparationAndReplacementFailures(t *testing.T) {
	for _, failure := range []string{"new preparation", "backup preparation", "current replacement"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newInstallerFixture(t, "")
			current := filepath.Join(fixture.binDir, "daem")
			old, previous := []byte("retained current\n"), []byte("retained previous\n")
			writeFixtureFile(t, current, old, 0o755)
			writeFixtureFile(t, current+".previous", previous, 0o755)
			tool, target := "install", "*/.daem-install.*/daem"
			if failure == "backup preparation" {
				target = "*/.daem-install.*/previous"
			}
			if failure == "current replacement" {
				tool, target = "mv", shellQuote(current)
			}
			adapter := "#!/bin/sh\nfor arg do destination=$arg; done\ncase \"$destination\" in " + target + ") exit 74;; esac\nexec /usr/bin/" + tool + " \"$@\"\n"
			if tool == "mv" {
				adapter = strings.ReplaceAll(adapter, "/usr/bin/mv", "/bin/mv")
			}
			writeFixtureFile(t, filepath.Join(fixture.root, "adapters", tool), []byte(adapter), 0o700)
			fixture.run(t, false, "--version", fixtureVersion)
			assertFile(t, current, old)
			if failure == "current replacement" {
				previous = old
			}
			assertFile(t, current+".previous", previous)
		})
	}
}

func TestInstallerRepairsNonExecutableDestinationsWithoutReplacingBackup(t *testing.T) {
	for _, kind := range []string{"regular file", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newInstallerFixture(t, "")
			current := filepath.Join(fixture.binDir, "daem")
			previous := fixture.releases[fixtureNext].binary
			writeFixtureFile(t, current+".previous", previous, 0o755)
			if kind == "regular file" {
				writeFixtureFile(t, current, fixture.releases[fixtureVersion].binary, 0o644)
			} else if err := os.Symlink(filepath.Join(fixture.root, "missing-binary"), current); err != nil {
				t.Fatal(err)
			}
			fixture.run(t, true, "--version", fixtureVersion)
			assertFile(t, current, fixture.releases[fixtureVersion].binary)
			assertFile(t, current+".previous", previous)
			info, err := os.Lstat(current)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("repaired destination is not a regular executable: %v, %v", info, err)
			}
		})
	}
}

func TestInstallerRefusesDirectoryShapedDestinations(t *testing.T) {
	for _, name := range []string{"daem", "daem.previous"} {
		t.Run(name, func(t *testing.T) {
			fixture := newInstallerFixture(t, "")
			directory := filepath.Join(fixture.binDir, name)
			writeFixtureFile(t, filepath.Join(directory, "retained"), []byte("retained"), 0o600)
			fixture.run(t, false, "--version", fixtureVersion)
			assertFile(t, filepath.Join(directory, "retained"), []byte("retained"))
			assertAbsent(t, filepath.Join(directory, "daem"))
		})
	}
}

func TestInstallerTerminationCleansStagingAndPreservesExecutables(t *testing.T) {
	fixture := newInstallerFixture(t, "")
	current := filepath.Join(fixture.binDir, "daem")
	writeFixtureFile(t, current, []byte("current"), 0o755)
	writeFixtureFile(t, current+".previous", []byte("previous"), 0o755)
	curl := filepath.Join(fixture.root, "adapters", "curl")
	adapter, err := os.ReadFile(curl)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.root, "observations", "signaled")
	interrupt := "for arg do\ncase \"$arg\" in */releases/download/*) kill -TERM \"$PPID\" || exit 94; printf signaled > " + shellQuote(marker) + "; exit 143;; esac\ndone\n"
	writeFixtureFile(t, curl, bytes.Replace(adapter, []byte("exec "), []byte(interrupt+"exec "), 1), 0o700)
	fixture.run(t, false, "--version", fixtureVersion)
	assertFile(t, marker, []byte("signaled"))
	assertFile(t, current, []byte("current"))
	assertFile(t, current+".previous", []byte("previous"))
}

func TestInstallerRollbackFailurePreservesCurrent(t *testing.T) {
	fixture := newInstallerFixture(t, "")
	current := filepath.Join(fixture.binDir, "daem")
	writeFixtureFile(t, current, []byte("retained"), 0o755)
	fixture.run(t, false, "--rollback")
	assertFile(t, current, []byte("retained"))
	writeFixtureFile(t, current+".previous", []byte("#!/bin/sh\nexit 1\n"), 0o755)
	fixture.run(t, false, "--rollback")
	assertFile(t, current, []byte("retained"))
	if fixture.requestCount("") != 0 {
		t.Fatal("rollback contacted a release endpoint")
	}
}
