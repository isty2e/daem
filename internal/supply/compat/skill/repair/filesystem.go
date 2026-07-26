package repair

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
)

type skillDocumentFileState struct {
	Content []byte
	Hash    artifact.ContentHash
	Mode    uint32
}

func skillDocumentState(
	ctx context.Context,
	root string,
	relativePath string,
) (skillDocumentFileState, error) {
	view, err := access.OpenView(root)
	if err != nil {
		return skillDocumentFileState{}, err
	}
	content, err := skillcompat.ReadSkillDocument(ctx, view, relativePath)
	if err != nil {
		return skillDocumentFileState{}, fmt.Errorf("read %s: %w", relativePath, err)
	}
	bytes := content.Bytes()
	return skillDocumentFileState{
		Content: bytes,
		Hash:    artifact.HashFileContent(bytes),
		Mode:    uint32(content.Mode().Perm()),
	}, nil
}

func checkSkillDocumentReplacementSize(inputSize int, oldSize int, newSize int) error {
	if inputSize < 0 || oldSize < 0 || oldSize > inputSize || newSize < 0 {
		return fmt.Errorf("skill document replacement sizes are invalid")
	}
	return skillcompat.CheckSkillDocumentSize(
		int64(inputSize-oldSize) + int64(newSize),
	)
}

func artifactPath(root string, relativePath string) (string, error) {
	if err := validateRecipePath(relativePath); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(relativePath)), nil
}

func replaceAt(content []byte, offset int, oldBytes []byte, newBytes []byte) []byte {
	repaired := make([]byte, 0, len(content)-len(oldBytes)+len(newBytes))
	repaired = append(repaired, content[:offset]...)
	repaired = append(repaired, newBytes...)
	repaired = append(repaired, content[offset+len(oldBytes):]...)
	return repaired
}

func validateInstallName(installName string) error {
	if strings.TrimSpace(installName) == "" ||
		strings.TrimSpace(installName) != installName ||
		!utf8.ValidString(installName) ||
		installName == "." ||
		installName == ".." ||
		strings.HasPrefix(installName, "~") ||
		strings.Contains(installName, "/") ||
		strings.Contains(installName, "\\") ||
		strings.IndexFunc(installName, func(character rune) bool {
			return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
		}) >= 0 ||
		filepath.Clean(installName) != installName {
		return fmt.Errorf("skill compatibility repair requires a safe single-segment skill name")
	}
	return nil
}

func writeExistingFile(path string, content []byte) (resultErr error) {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("repair target %q is not a regular file", path)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".daem-skill-repair-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, tempFile.Close())
		}
		if !committed {
			if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove repair staging file: %w", err))
			}
		}
	}()
	if _, err := tempFile.Write(content); err != nil {
		return err
	}
	if err := tempFile.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}
