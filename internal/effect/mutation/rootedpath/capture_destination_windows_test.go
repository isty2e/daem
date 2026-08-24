//go:build windows

package rootedpath

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func longWindowsPathName(path string) (string, error) {
	short, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	for size := uint32(256); size <= 1<<15; size *= 2 {
		buffer := make([]uint16, size)
		written, err := windows.GetLongPathName(short, &buffer[0], size)
		if err != nil {
			return "", err
		}
		if written == 0 {
			continue
		}
		if written < size {
			return windows.UTF16ToString(buffer[:written]), nil
		}
	}
	return "", fmt.Errorf("long path buffer exhausted for %q", path)
}

func createWindowsTestJunction(t *testing.T, link string, target string) {
	t.Helper()
	output, err := exec.Command("cmd", "/C", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction %q -> %q: %v: %s", link, target, err, output)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

func TestCaptureDestinationNoFollowRejectsReparseAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	alias := filepath.Join(root, "alias")
	createWindowsTestJunction(t, alias, outside)

	_, _, err := CaptureDestinationNoFollow(filepath.Join(alias, "state", "state.json"))
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("CaptureDestinationNoFollow error = %v, want rooted-path failure", err)
	}
	if failure.Kind() != FailureRootReplaced {
		t.Fatalf("failure kind = %v, want %v", failure.Kind(), FailureRootReplaced)
	}
}

func TestCaptureDestinationNoFollowBindsCleanAncestor(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "state", "state.json")
	captured, bound, err := CaptureDestinationNoFollow(destination)
	if err != nil {
		t.Fatalf("CaptureDestinationNoFollow: %v", err)
	}
	defer captured.Close()
	lexical, err := bound.LexicalPath()
	if err != nil {
		t.Fatal(err)
	}
	expectedDestination := destination
	if longRoot, longErr := longWindowsPathName(root); longErr == nil {
		expectedDestination = filepath.Join(longRoot, "state", "state.json")
	}
	if filepath.Clean(lexical) != filepath.Clean(expectedDestination) {
		t.Fatalf("lexical path = %q, want %q", lexical, expectedDestination)
	}
	if bound.Relative().Path() != "state/state.json" {
		t.Fatalf("relative destination = %q, want state/state.json", bound.Relative().Path())
	}
}

func TestCaptureDestinationPreservesAliasResolutionSemantics(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	alias := filepath.Join(root, "alias")
	createWindowsTestJunction(t, alias, outside)

	captured, bound, err := CaptureDestination(filepath.Join(alias, "state", "state.json"))
	if err != nil {
		t.Fatalf("CaptureDestination: %v", err)
	}
	defer captured.Close()
	if bound.Relative().Path() != "state/state.json" {
		t.Fatalf("relative destination = %q, want state/state.json", bound.Relative().Path())
	}
	if strings.EqualFold(bound.Root().PhysicalRoot(), alias) {
		t.Fatalf("physical root %q retained the alias spelling", bound.Root().PhysicalRoot())
	}
}
