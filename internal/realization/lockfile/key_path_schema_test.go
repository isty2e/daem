package lockfile

import (
	"errors"
	"testing"
)

func TestCurrentLockRejectsLegacyMixedMissingAndInvalidKeyPath(t *testing.T) {
	canonical := marshalLockfileForTest(t, lockfileWithSubjects(t, claudeProjectMCPSubjectContract(t)))
	for _, replacement := range []string{
		`content_path = "/mcpServers/context7"`,
		"key_path = \"/mcpServers/context7\"\ncontent_path = \"/mcpServers/context7\"",
		"",
		`key_path = ""`,
		`key_path = "/mcpServers/bad~2"`,
	} {
		content := replaceLockfileStringOnce(t, canonical, `key_path = "/mcpServers/context7"`, replacement)
		file, err := loadContent(t.Context(), []byte(content))
		if err == nil || file.Version != 0 || len(file.Locked.Subjects()) != 0 {
			t.Fatal("invalid current key-path wire acquired partial lock authority")
		}
	}
}

func TestPreviousLockBodyIsOpaqueOnlyForRegeneration(t *testing.T) {
	content := []byte("version = 6\nlegacy_payload = { malformed = [\n")
	file, err := loadContent(t.Context(), content)
	var versionErr UnsupportedVersionError
	if !errors.As(err, &versionErr) || versionErr.Found != 6 || !versionErr.RelockSupported() || file.Version != 0 {
		t.Fatalf("previous lock body acquired authority or lost regeneration policy: %v", err)
	}
	if err := validateReplacementContent(t.Context(), content); err != nil {
		t.Fatalf("previous lock body was interpreted during authorized regeneration: %v", err)
	}
}
