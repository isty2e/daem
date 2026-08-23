//go:build windows

package rootedpath

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	if filepath.Clean(lexical) != filepath.Clean(destination) {
		t.Fatalf("lexical path = %q, want %q", lexical, destination)
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
