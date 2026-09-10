package install_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerNativeRelease(t *testing.T) {
	directory := os.Getenv("DAEM_TEST_RELEASE_DIR")
	if directory == "" {
		t.Skip("native release rehearsal requires an explicit artifact directory and expected release facts")
	}
	required := func(name string) string {
		value := os.Getenv(name)
		if value == "" {
			t.Fatalf("native release rehearsal requires %s", name)
		}
		return value
	}
	release := releaseFixture{
		version:   required("DAEM_TEST_RELEASE_VERSION"),
		revision:  required("DAEM_TEST_RELEASE_REVISION"),
		timestamp: required("DAEM_TEST_RELEASE_TIME"),
		toolchain: required("DAEM_TEST_RELEASE_TOOLCHAIN"),
		target:    required("DAEM_TEST_RELEASE_TARGET"),
	}
	var err error
	release.archive, err = os.ReadFile(filepath.Join(directory, release.archiveName()))
	if err != nil {
		t.Fatal(err)
	}
	release.sidecar, err = os.ReadFile(filepath.Join(directory, release.archiveName()+".sha256"))
	if err != nil {
		t.Fatal(err)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(release.archive))
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil || header.Name != "daem" || header.Typeflag != tar.TypeReg {
		t.Fatalf("native archive first entry = %v, error=%v", header, err)
	}
	release.binary, err = io.ReadAll(tarReader)
	if err != nil {
		t.Fatal(err)
	}
	development, err := os.ReadFile(required("DAEM_TEST_DEVELOPMENT_BINARY"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(development, release.binary) {
		t.Fatal("native upgrade rehearsal requires a distinct development binary")
	}
	newNativeFixture := func(t *testing.T, candidate releaseFixture) *installerFixture {
		t.Helper()
		fixture := newInstallerFixture(t, "", candidate)
		for _, name := range []string{"uname", "sysctl", "sw_vers"} {
			if err := os.Remove(filepath.Join(fixture.root, "adapters", name)); err != nil {
				t.Fatal(err)
			}
		}
		return fixture
	}
	t.Run("fresh", func(t *testing.T) {
		fixture := newNativeFixture(t, release)
		current := filepath.Join(fixture.binDir, "daem")
		fixture.run(t, true, "--version", release.version)
		assertFile(t, current, release.binary)
		fixture.run(t, true, "--version", release.version)
		assertAbsent(t, current+".previous")
	})
	t.Run("development upgrade and offline rollback", func(t *testing.T) {
		fixture := newNativeFixture(t, release)
		current := filepath.Join(fixture.binDir, "daem")
		writeFixtureFile(t, current, development, 0o755)
		fixture.run(t, true, "--version", release.version)
		assertFile(t, current, release.binary)
		assertFile(t, current+".previous", development)
		fixture.run(t, true, "--version", release.version)
		assertFile(t, current+".previous", development)
		fixture.server.Close()
		fixture.run(t, true, "--rollback")
		assertFile(t, current, development)
	})
	for _, fault := range []string{"checksum", "identity"} {
		t.Run(fault+" refusal", func(t *testing.T) {
			candidate := release
			diagnostic := "exact checksum entry"
			if fault == "checksum" {
				candidate.sidecar = bytes.Clone(release.sidecar)
				if candidate.sidecar[0] == '0' {
					candidate.sidecar[0] = '1'
				} else {
					candidate.sidecar[0] = '0'
				}
			} else {
				candidate.archive = executableArchive(t, development, false)
				candidate.sidecar = nil
				diagnostic = "requested release identity"
			}
			fixture := newNativeFixture(t, candidate)
			current := filepath.Join(fixture.binDir, "daem")
			writeFixtureFile(t, current, release.binary, 0o755)
			writeFixtureFile(t, current+".previous", development, 0o755)
			output := fixture.run(t, false, "--version", release.version)
			if !strings.Contains(output, diagnostic) {
				t.Fatalf("wrong native refusal, want %q:\n%s", diagnostic, output)
			}
			assertFile(t, current, release.binary)
			assertFile(t, current+".previous", development)
		})
	}
}
