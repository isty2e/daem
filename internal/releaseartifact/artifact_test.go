package releaseartifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/buildidentity"
	"github.com/isty2e/daem/internal/platformsupport"
)

func TestReleaseArtifactContract(t *testing.T) {
	fixture := buildArtifactFixture(t)

	t.Run("deterministic normalized archive", func(t *testing.T) {
		t.Setenv("TZ", "Pacific/Honolulu")
		t.Setenv("LANG", "C")
		t.Setenv("SOURCE_DATE_EPOCH", "0")
		first, err := Build(fixture.executable, fixture.requirement, fixture.target)
		if err != nil {
			t.Fatalf("Build returned error: %v", err)
		}
		t.Setenv("TZ", "Asia/Seoul")
		t.Setenv("LANG", "ko_KR.UTF-8")
		t.Setenv("SOURCE_DATE_EPOCH", "9999999999")
		second, err := Build(fixture.executable, fixture.requirement, fixture.target)
		if err != nil {
			t.Fatalf("second Build returned error: %v", err)
		}
		if !bytes.Equal(first.ArchiveBytes(), second.ArchiveBytes()) {
			t.Fatal("identical inputs produced different archives")
		}
		wantName := "daem_1.2.3_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
		if first.ArchiveName() != wantName || first.ChecksumName() != wantName+".sha256" {
			t.Fatalf("artifact names = %q/%q, want %q", first.ArchiveName(), first.ChecksumName(), wantName)
		}
		assertArchiveContract(t, first.ArchiveBytes(), fixture.executable, fixture.revisionAt)
		assertChecksumContract(t, first)
	})

	t.Run("accessors are defensive", func(t *testing.T) {
		artifact, err := Build(fixture.executable, fixture.requirement, fixture.target)
		if err != nil {
			t.Fatalf("Build returned error: %v", err)
		}
		archive := artifact.ArchiveBytes()
		archive[0] ^= 0xff
		if bytes.Equal(archive, artifact.ArchiveBytes()) {
			t.Fatal("archive accessor exposed mutable owned bytes")
		}
		checksum := artifact.ChecksumBytes()
		checksum[0] = 'x'
		if checksum[0] == artifact.ChecksumBytes()[0] {
			t.Fatal("checksum accessor exposed mutable owned bytes")
		}
	})

	t.Run("binary content changes digest", func(t *testing.T) {
		original, err := Build(fixture.executable, fixture.requirement, fixture.target)
		if err != nil {
			t.Fatalf("Build returned error: %v", err)
		}
		withTrailer := append(append([]byte(nil), fixture.executable...), []byte("release-artifact-test-trailer")...)
		changed, err := Build(withTrailer, fixture.requirement, fixture.target)
		if err != nil {
			t.Fatalf("Build rejected executable-preserving trailer: %v", err)
		}
		if changed.SHA256() == original.SHA256() {
			t.Fatal("changed executable bytes retained the archive digest")
		}
	})

	t.Run("invalid and mismatched facts fail closed", func(t *testing.T) {
		requirements := []buildidentity.ReleaseRequirement{
			mustArtifactRequirement(t, "v1.2.4", fixture.revision, runtime.Version()),
			mustArtifactRequirement(t, "v1.2.3", strings.Repeat("a", 40), runtime.Version()),
			mustArtifactRequirement(t, "v1.2.3", fixture.revision, alternateGoVersion()),
			{},
		}
		for index, requirement := range requirements {
			if _, err := Build(fixture.executable, requirement, fixture.target); err == nil {
				t.Fatalf("mismatched requirement[%d] succeeded", index)
			}
		}

		otherTarget := alternateAdmittedTarget(t)
		if _, err := Build(fixture.executable, fixture.requirement, otherTarget); err == nil || !strings.Contains(err.Error(), "target") {
			t.Fatalf("mismatched target error = %v", err)
		}
		if _, err := Build(fixture.executable, fixture.requirement, platformsupport.Target{}); err == nil {
			t.Fatal("zero expected target succeeded")
		}
		if _, err := Build([]byte("not a Go executable"), fixture.requirement, fixture.target); err == nil {
			t.Fatal("non-Go bytes succeeded")
		}
	})

	t.Run("dirty executable fails closed", func(t *testing.T) {
		writeArtifactFixtureFile(t, fixture.mainPath, "package main\nfunc main() {}\n// modified\n")
		dirtyPath := filepath.Join(fixture.root, "daem-dirty")
		buildArtifactFixtureExecutable(t, fixture.repository, dirtyPath, runtime.GOOS, runtime.GOARCH)
		dirtyBytes, err := os.ReadFile(dirtyPath)
		if err != nil {
			t.Fatalf("read dirty executable: %v", err)
		}
		if _, err := Build(dirtyBytes, fixture.requirement, fixture.target); err == nil {
			t.Fatal("dirty executable succeeded")
		}
	})

	t.Run("compile-only target is not an artifact target", func(t *testing.T) {
		writeArtifactFixtureFile(t, fixture.mainPath, "package main\nfunc main() {}\n")
		compileOnly, err := platformsupport.ParseTarget("darwin", "amd64")
		if err != nil {
			t.Fatalf("ParseTarget returned error: %v", err)
		}
		path := filepath.Join(fixture.root, "daem-darwin-amd64")
		buildArtifactFixtureExecutable(t, fixture.repository, path, compileOnly.OS(), compileOnly.Arch())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read compile-only executable: %v", err)
		}
		if _, err := Build(content, fixture.requirement, compileOnly); err == nil || !strings.Contains(err.Error(), "not release-admitted") {
			t.Fatalf("compile-only Build error = %v", err)
		}
	})
}

func TestZeroArtifactFailsClosed(t *testing.T) {
	var artifact Artifact
	if artifact.ArchiveName() != "" || artifact.ChecksumName() != "" || artifact.SHA256() != "" {
		t.Fatalf("zero artifact names/digest = %q/%q/%q", artifact.ArchiveName(), artifact.ChecksumName(), artifact.SHA256())
	}
	if artifact.ArchiveBytes() != nil || artifact.ChecksumBytes() != nil {
		t.Fatal("zero artifact exposed content")
	}
}

type artifactFixture struct {
	root        string
	repository  string
	mainPath    string
	executable  []byte
	revision    string
	revisionAt  time.Time
	requirement buildidentity.ReleaseRequirement
	target      platformsupport.Target
}

func buildArtifactFixture(t *testing.T) artifactFixture {
	t.Helper()
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("release artifact integration requires git: %v", err)
	}
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	mainPath := filepath.Join(repository, "cmd", "daem", "main.go")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		t.Fatalf("create fixture repository: %v", err)
	}
	writeArtifactFixtureFile(t, filepath.Join(repository, "go.mod"), "module "+buildidentity.MainModulePath+"\n\ngo 1.25.0\n")
	writeArtifactFixtureFile(t, mainPath, "package main\nfunc main() {}\n")

	runArtifactCommand(t, repository, nil, gitBinary, "init", "--quiet")
	runArtifactCommand(t, repository, nil, gitBinary, "config", "user.name", "Release Artifact Test")
	runArtifactCommand(t, repository, nil, gitBinary, "config", "user.email", "release-artifact@example.invalid")
	runArtifactCommand(t, repository, nil, gitBinary, "add", ".")
	commitEnvironment := map[string]string{
		"GIT_AUTHOR_DATE":    "2026-07-01T02:03:04Z",
		"GIT_COMMITTER_DATE": "2026-07-01T02:03:04Z",
	}
	runArtifactCommand(t, repository, commitEnvironment, gitBinary, "commit", "--quiet", "-m", "fixture")
	runArtifactCommand(t, repository, nil, gitBinary, "tag", "v1.2.3")
	revision := strings.TrimSpace(runArtifactCommand(t, repository, nil, gitBinary, "rev-parse", "HEAD"))

	executablePath := filepath.Join(root, "daem")
	buildArtifactFixtureExecutable(t, repository, executablePath, runtime.GOOS, runtime.GOARCH)
	executable, err := os.ReadFile(executablePath)
	if err != nil {
		t.Fatalf("read fixture executable: %v", err)
	}
	target, err := platformsupport.ParseTarget(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("ParseTarget returned error: %v", err)
	}
	requirement := mustArtifactRequirement(t, "v1.2.3", revision, runtime.Version())
	revisionAt, err := time.Parse(time.RFC3339, "2026-07-01T02:03:04Z")
	if err != nil {
		t.Fatalf("parse fixture revision time: %v", err)
	}
	return artifactFixture{
		root: root, repository: repository, mainPath: mainPath,
		executable: executable, revision: revision, revisionAt: revisionAt,
		requirement: requirement, target: target,
	}
}

func buildArtifactFixtureExecutable(t *testing.T, repository string, outputPath string, goos string, goarch string) {
	t.Helper()
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	environment := map[string]string{
		"CGO_ENABLED": "0", "GOFLAGS": "", "GOOS": goos, "GOARCH": goarch,
		"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
	}
	runArtifactCommand(t, repository, environment, goBinary, "build", "-buildvcs=true", "-mod=readonly", "-trimpath", "-o", outputPath, "./cmd/daem")
}

func assertArchiveContract(t *testing.T, compressed []byte, executable []byte, revisionAt time.Time) {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer reader.Close()
	if !reader.ModTime.Equal(revisionAt) || reader.Name != "" || reader.Comment != "" || reader.OS != 255 {
		t.Fatalf("gzip header = time=%v name=%q comment=%q os=%d", reader.ModTime, reader.Name, reader.Comment, reader.OS)
	}

	archive := tar.NewReader(reader)
	header, err := archive.Next()
	if err != nil {
		t.Fatalf("read tar header: %v", err)
	}
	if header.Name != "daem" || header.Typeflag != tar.TypeReg || header.Mode != 0o755 || header.Size != int64(len(executable)) {
		t.Fatalf("tar header identity = name=%q type=%d mode=%#o size=%d", header.Name, header.Typeflag, header.Mode, header.Size)
	}
	if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" || header.Format != tar.FormatUSTAR {
		t.Fatalf("tar ownership/format = uid=%d gid=%d uname=%q gname=%q format=%v", header.Uid, header.Gid, header.Uname, header.Gname, header.Format)
	}
	if !header.ModTime.Equal(revisionAt) || !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
		t.Fatalf("tar times = mod=%v access=%v change=%v", header.ModTime, header.AccessTime, header.ChangeTime)
	}
	content, err := io.ReadAll(archive)
	if err != nil {
		t.Fatalf("read archive member: %v", err)
	}
	if !bytes.Equal(content, executable) {
		t.Fatal("archive member differs from executable input")
	}
	if header, err := archive.Next(); err != io.EOF || header != nil {
		t.Fatalf("second archive member = %#v, err=%v", header, err)
	}
}

func assertChecksumContract(t *testing.T, artifact Artifact) {
	t.Helper()
	digest := sha256.Sum256(artifact.ArchiveBytes())
	wantDigest := hex.EncodeToString(digest[:])
	if artifact.SHA256() != wantDigest {
		t.Fatalf("SHA256 = %q, want %q", artifact.SHA256(), wantDigest)
	}
	wantSidecar := wantDigest + "  " + artifact.ArchiveName() + "\n"
	if string(artifact.ChecksumBytes()) != wantSidecar {
		t.Fatalf("checksum sidecar = %q, want %q", artifact.ChecksumBytes(), wantSidecar)
	}
}

func alternateAdmittedTarget(t *testing.T) platformsupport.Target {
	t.Helper()
	goos, goarch := "darwin", "arm64"
	if runtime.GOOS == goos && runtime.GOARCH == goarch {
		goos, goarch = "linux", "amd64"
	}
	target, err := platformsupport.ParseTarget(goos, goarch)
	if err != nil {
		t.Fatalf("ParseTarget returned error: %v", err)
	}
	return target
}

func alternateGoVersion() string {
	if runtime.Version() == "go1.25.12" {
		return "go1.26.5"
	}
	return "go1.25.12"
}

func mustArtifactRequirement(t *testing.T, version string, revision string, goVersion string) buildidentity.ReleaseRequirement {
	t.Helper()
	requirement, err := buildidentity.NewReleaseRequirement(version, revision, goVersion)
	if err != nil {
		t.Fatalf("NewReleaseRequirement returned error: %v", err)
	}
	return requirement
}

func runArtifactCommand(t *testing.T, directory string, overrides map[string]string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	command.Env = artifactEnvironment(overrides)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func artifactEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	maps.Copy(values, overrides)
	environment := make([]string, 0, len(values))
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func writeArtifactFixtureFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
