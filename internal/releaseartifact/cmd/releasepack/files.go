package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/releaseartifact"
)

func readStableRegularFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symlink", path)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("%s changed while it was opened", path)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) || int64(len(content)) != after.Size() {
		return nil, fmt.Errorf("%s changed while it was read", path)
	}
	return content, nil
}

func publishArtifactDirectory(outputDir string, artifact releaseartifact.Artifact) error {
	if err := validateArtifactBasenames(artifact); err != nil {
		return err
	}
	cleanOutput := filepath.Clean(outputDir)
	base := filepath.Base(cleanOutput)
	if cleanOutput == filepath.Dir(cleanOutput) || base == "." || base == string(filepath.Separator) {
		return fmt.Errorf("output directory must name a new child directory")
	}
	if _, err := os.Lstat(cleanOutput); err == nil {
		return fmt.Errorf("output path already exists: %s", cleanOutput)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(cleanOutput)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", parent)
	}

	staging, err := os.MkdirTemp(parent, "."+base+".tmp-")
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}
	if err := writeCompleteFile(filepath.Join(staging, artifact.ArchiveName()), artifact.ArchiveBytes()); err != nil {
		return err
	}
	if err := writeCompleteFile(filepath.Join(staging, artifact.ChecksumName()), artifact.ChecksumBytes()); err != nil {
		return err
	}
	if _, err := os.Lstat(cleanOutput); err == nil {
		return fmt.Errorf("output path appeared during assembly: %s", cleanOutput)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, cleanOutput); err != nil {
		return err
	}
	published = true
	return nil
}

func validateArtifactBasenames(artifact releaseartifact.Artifact) error {
	archiveName := artifact.ArchiveName()
	checksumName := artifact.ChecksumName()
	if archiveName == "" || filepath.Base(archiveName) != archiveName {
		return fmt.Errorf("invalid archive basename %q", archiveName)
	}
	if checksumName != archiveName+".sha256" || filepath.Base(checksumName) != checksumName {
		return fmt.Errorf("invalid checksum basename %q", checksumName)
	}
	if len(artifact.ArchiveBytes()) == 0 || len(artifact.ChecksumBytes()) == 0 {
		return fmt.Errorf("artifact content is empty")
	}
	return nil
}

func writeCompleteFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
