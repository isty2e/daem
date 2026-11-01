//go:build windows

package rootedpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type referentFrameTestBudget struct {
	visits int
	at     int
	change func() error
	err    error
}

func (budget *referentFrameTestBudget) AdmitPathComponents(count int) error {
	budget.visits += count
	if budget.visits == budget.at {
		budget.err = budget.change()
	}
	return budget.err
}

func TestWindowsReferentLeafCompletesPendingReparseVerification(t *testing.T) {
	base := referentTestRoot(t)
	for _, name := range []string{"original", "other"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	referentTestSymlink(t, "original", alias)
	_, components, err := prepareWindowsAbsolutePath(alias, nil)
	if err != nil {
		t.Fatal(err)
	}
	budget := &referentFrameTestBudget{at: len(components) + 1, change: func() error {
		if err := os.Remove(alias); err != nil {
			return err
		}
		return os.Symlink("other", alias)
	}}
	resolved, err := ResolveReferentPathBounded(alias, 256, budget)
	if budget.visits != budget.at || budget.err != nil {
		t.Fatalf("retarget fixture did not reach the recorded frame: visits=%d, err=%v", budget.visits, budget.err)
	}
	if resolved != "" || !errors.Is(err, errWindowsReparseChanged) {
		t.Fatalf("changed final alias returned %q, %v; want frame revalidation refusal", resolved, err)
	}
}

func TestWindowsReferentLeafRevalidationChecksFileRatherThanParent(t *testing.T) {
	base := referentTestRoot(t)
	file := filepath.Join(base, "settings")
	if err := os.WriteFile(file, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, platform, _, _, _, err := resolveDirectoryPathPlatform(base, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeCapturedRootPlatform(&platform)
	parent := platform.directories[len(platform.directories)-1]
	handle, facts, err := openWindowsChild(parent.handle, parent.caseSensitive, "settings")
	if err != nil {
		t.Fatal(err)
	}
	defer closeWindowsHandle(handle)
	leaf := &windowsResolvedLeaf{name: "settings", handle: handle, facts: facts}
	if err := verifyWindowsResolvedTarget(&platform, leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyWindowsResolvedTarget(&platform, leaf); !errors.Is(err, errWindowsReparseChanged) {
		t.Fatalf("replaced file with unchanged parent returned %v", err)
	}
}

func TestWindowsReferentRefusesFileTargetsWithTrailingSeparator(t *testing.T) {
	base := referentTestRoot(t)
	file := filepath.Join(base, "settings")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	referentTestSymlink(t, "settings", filepath.Join(base, "capability-alias"))

	for _, test := range []struct {
		name   string
		target string
	}{
		{name: "relative-backslash", target: `settings\`},
		{name: "relative-slash", target: "settings/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			alias := filepath.Join(base, test.name)
			if err := os.Symlink(test.target, alias); err != nil {
				t.Fatal(err)
			}
			if resolved, err := filepath.EvalSymlinks(alias); err == nil {
				t.Fatalf("directory-requiring alias unexpectedly resolves through the existing API to %q", resolved)
			}
			resolved, err := ResolveReferentPathBounded(alias, 256, &referentTestBudget{limit: 10_000})
			if resolved != "" || !hasFailureKind(err, FailureAncestorNotDirectory) {
				t.Fatalf("file target with trailing separator returned %q, %v", resolved, err)
			}
		})
	}
}

func TestWindowsReferentPreservesDirectoryReparseRequirement(t *testing.T) {
	base := referentTestRoot(t)
	directory := filepath.Join(base, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	referentTestSymlink(t, "directory", filepath.Join(base, "capability-alias"))

	alias := filepath.Join(base, "directory-alias")
	if err := os.Symlink(`directory\`, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveReferentPathBounded(alias, 256, &referentTestBudget{limit: 10_000})
	if err != nil || resolved != directory {
		t.Fatalf("valid directory target with trailing separator returned %q, %v", resolved, err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("directory", alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err = ResolveReferentPathBounded(alias, 256, &referentTestBudget{limit: 10_000})
	if resolved != "" || !hasFailureKind(err, FailureAncestorNotDirectory) {
		t.Fatalf("directory reparse point to a regular file returned %q, %v", resolved, err)
	}
}
