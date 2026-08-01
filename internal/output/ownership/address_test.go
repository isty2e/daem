package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestManagedAddressOverlapAlgebra(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.json")
	whole := mustAddress(t, config, "")
	servers := mustAddress(t, config, "/mcpServers")
	alpha := mustAddress(t, config, "/mcpServers/alpha")
	beta := mustAddress(t, config, "/mcpServers/beta")
	other := mustAddress(t, filepath.Join(root, "other.json"), "")
	prefixOnly := mustAddress(t, filepath.Join(root, "config.json.backup"), "")
	directory := mustAddress(t, filepath.Join(root, "skills"), "")
	child := mustAddress(t, filepath.Join(root, "skills", "reviewer"), "")

	tests := []struct {
		name  string
		left  ManagedAddress
		right ManagedAddress
		want  bool
	}{
		{name: "same whole path", left: whole, right: whole, want: true},
		{name: "whole overlaps projection", left: whole, right: alpha, want: true},
		{name: "projection parent overlaps child", left: servers, right: alpha, want: true},
		{name: "projection siblings are disjoint", left: alpha, right: beta, want: false},
		{name: "different files are disjoint", left: whole, right: other, want: false},
		{name: "physical string prefix is disjoint", left: whole, right: prefixOnly, want: false},
		{name: "physical parent overlaps child", left: directory, right: child, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.left.Overlaps(test.right); got != test.want {
				t.Fatalf("left.Overlaps(right) = %v, want %v", got, test.want)
			}
			if got := test.right.Overlaps(test.left); got != test.want {
				t.Fatalf("right.Overlaps(left) = %v, want %v", got, test.want)
			}
		})
	}
}

func TestManagedAddressRejectsInvalidValues(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name        string
		contentPath string
	}{
		{name: "root projection", contentPath: "/"},
		{name: "relative projection", contentPath: "mcpServers/alpha"},
		{name: "trailing slash", contentPath: "/mcpServers/"},
		{name: "empty segment", contentPath: "/mcpServers//alpha"},
		{name: "relative segment", contentPath: "/mcpServers/../alpha"},
		{name: "control character", contentPath: "/mcpServers/\nalpha"},
	}
	exact := mustPathAuthority(t, root)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewManagedAddress(exact, test.contentPath); err == nil {
				t.Fatal("NewManagedAddress returned nil error")
			}
		})
	}
}

func TestManagedAddressDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	alpha := mustAddress(t, filepath.Join(root, "a"), "/b")
	beta := mustAddress(t, filepath.Join(root, "a"), "/c")
	gamma := mustAddress(t, filepath.Join(root, "b"), "")
	if !alpha.Less(beta) || !beta.Less(gamma) || gamma.Less(alpha) {
		t.Fatal("ManagedAddress.Less does not provide canonical path/content order")
	}
}

func mustAddress(t *testing.T, path string, contentPath string) ManagedAddress {
	t.Helper()
	address, err := NewManagedAddress(mustPathAuthority(t, filepath.Clean(path)), contentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	return address
}

func mustPathAuthority(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	authority, err := pathauthority.NewExact(path, "exact-v1:")
	if err != nil {
		t.Fatalf("pathauthority.NewExact returned error: %v", err)
	}
	return authority
}
