// Package releaseartifact owns deterministic daem release archives and their
// checksum facts. It does not own file I/O, platform policy, CI evidence, or
// publication.
package releaseartifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/buildidentity"
	"github.com/isty2e/daem/internal/platformsupport"
)

const executableArchiveName = "daem"

// Artifact is one deterministic archive and its derived SHA-256 checksum. Its
// byte accessors return defensive copies.
type Artifact struct {
	archiveName string
	archive     []byte
	digest      [sha256.Size]byte
}

// Build parses and validates one daem executable, then constructs its
// deterministic admitted-target release artifact.
func Build(executable []byte, requirement buildidentity.ReleaseRequirement, expectedTarget platformsupport.Target) (Artifact, error) {
	info, err := buildinfo.Read(bytes.NewReader(executable))
	if err != nil {
		return Artifact{}, fmt.Errorf("read executable build identity: %w", err)
	}
	identity, err := buildidentity.FromBuildInfo(*info)
	if err != nil {
		return Artifact{}, fmt.Errorf("normalize executable build identity: %w", err)
	}
	if err := identity.RequireReleaseFacts(requirement); err != nil {
		return Artifact{}, fmt.Errorf("executable is not release-eligible: %w", err)
	}
	if identity.Target() != expectedTarget {
		return Artifact{}, fmt.Errorf("executable target is %s, want %s", identity.Target(), expectedTarget)
	}
	admission, err := platformsupport.Lookup(identity.Target().OS(), identity.Target().Arch())
	if err != nil {
		return Artifact{}, fmt.Errorf("look up executable target admission: %w", err)
	}
	if err := admission.RequireSupported(); err != nil {
		return Artifact{}, fmt.Errorf("executable target is not release-admitted: %w", err)
	}

	revisionAt, ok := identity.RevisionTime()
	if !ok {
		return Artifact{}, fmt.Errorf("release-eligible executable has no revision time")
	}
	archive, err := deterministicArchive(executable, revisionAt)
	if err != nil {
		return Artifact{}, err
	}
	archiveName := fmt.Sprintf(
		"daem_%s_%s_%s.tar.gz",
		strings.TrimPrefix(requirement.Version(), "v"),
		expectedTarget.OS(),
		expectedTarget.Arch(),
	)
	return Artifact{
		archiveName: archiveName,
		archive:     archive,
		digest:      sha256.Sum256(archive),
	}, nil
}

// ArchiveName returns the stable archive basename.
func (artifact Artifact) ArchiveName() string { return artifact.archiveName }

// ArchiveBytes returns a defensive copy of the compressed archive.
func (artifact Artifact) ArchiveBytes() []byte {
	return append([]byte(nil), artifact.archive...)
}

// SHA256 returns the lowercase hexadecimal archive digest.
func (artifact Artifact) SHA256() string {
	if artifact.archiveName == "" {
		return ""
	}
	return hex.EncodeToString(artifact.digest[:])
}

// ChecksumName returns the stable SHA-256 sidecar basename.
func (artifact Artifact) ChecksumName() string {
	if artifact.archiveName == "" {
		return ""
	}
	return artifact.archiveName + ".sha256"
}

// ChecksumBytes returns the checksum sidecar content.
func (artifact Artifact) ChecksumBytes() []byte {
	if artifact.archiveName == "" {
		return nil
	}
	return []byte(artifact.SHA256() + "  " + artifact.archiveName + "\n")
}

func deterministicArchive(executable []byte, revisionAt time.Time) ([]byte, error) {
	var output bytes.Buffer
	compressed, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create gzip writer: %w", err)
	}
	compressed.Header.ModTime = revisionAt.UTC()
	compressed.Header.Name = ""
	compressed.Header.Comment = ""
	compressed.Header.OS = 255

	archive := tar.NewWriter(compressed)
	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     executableArchiveName,
		Mode:     0o755,
		Size:     int64(len(executable)),
		ModTime:  revisionAt.UTC(),
		Format:   tar.FormatUSTAR,
	}
	if err := archive.WriteHeader(header); err != nil {
		_ = archive.Close()
		_ = compressed.Close()
		return nil, fmt.Errorf("write tar header: %w", err)
	}
	if _, err := io.Copy(archive, bytes.NewReader(executable)); err != nil {
		_ = archive.Close()
		_ = compressed.Close()
		return nil, fmt.Errorf("write tar executable: %w", err)
	}
	if err := archive.Close(); err != nil {
		_ = compressed.Close()
		return nil, fmt.Errorf("close tar archive: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return nil, fmt.Errorf("close gzip archive: %w", err)
	}
	return output.Bytes(), nil
}
