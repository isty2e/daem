//go:build darwin || linux

package recovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestPermissionRecoveryUsesOnlyTheRecordedMCPPhysicalPoint(t *testing.T) {
	for _, state := range []string{"present", "missing", "retargeted", "ordinary-retargeted", "unlocatable-fold", "unlocatable-fold-missing"} {
		t.Run(state, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			literal := filepath.Join(home, ".claude.json")
			if err := os.WriteFile(literal, []byte("literal foreign content\n"), 0o660); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(literal, 0o660); err != nil {
				t.Fatal(err)
			}
			name := "carrier-Cafe\u0301/settings.json"
			if state == "unlocatable-fold" || state == "unlocatable-fold-missing" {
				name = "carrier-İ/settings.json"
			}
			fixture := ownershiptest.PermissionRecovery(t, name)
			before := fixture.Protection.Documents()[0]
			destination, err := output.Parse(aggregate.ClaudeGlobalMCPConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := ownership.NewDocumentBinding(fixture.Owner, destination, before.PathAuthority())
			if err != nil {
				t.Fatal(err)
			}
			bindings := []ownership.DocumentBinding{binding}
			var ordinaryAlias string
			if state == "ordinary-retargeted" {
				ordinaryAlias = filepath.Join(fixture.Paths.ManifestRoot, ".pi", "settings.json")
				if err := os.MkdirAll(filepath.Dir(ordinaryAlias), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(fixture.Documents[0], ordinaryAlias); err != nil {
					t.Fatal(err)
				}
				ordinaryDestination, err := output.Parse(".pi/settings.json")
				if err != nil {
					t.Fatal(err)
				}
				ordinary, err := ownership.NewDocumentBinding(fixture.Owner, ordinaryDestination, before.PathAuthority())
				if err != nil {
					t.Fatal(err)
				}
				bindings = append(bindings, ordinary)
			}
			document, err := ownership.NewProtectedDocument(bindings, before.UserID(), before.GroupID(), before.ModeCap())
			if err != nil {
				t.Fatal(err)
			}
			row, err := ownership.NewArmedProtection(fixture.Owner, fixture.Protection.Invocation(), fixture.Protection.Correlation(), []ownership.ProtectedDocument{document})
			if err != nil {
				t.Fatal(err)
			}
			registry, err := ownership.NewRegistry(nil, bindings...)
			if err != nil {
				t.Fatal(err)
			}
			arm, err := ownership.NewArmProtection(row)
			if err != nil {
				t.Fatal(err)
			}
			registry, _, err = registry.ApplyProtection(fixture.Owner, arm)
			if err != nil {
				t.Fatal(err)
			}
			content, err := ownershipstore.Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.Paths.OwnershipRegistryPath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			physical := fixture.Documents[0]
			var foldedPeer string
			if state == "unlocatable-fold" || state == "unlocatable-fold-missing" {
				foldedPeer = filepath.Join(fixture.Paths.ManifestRoot, "carrier-i", "settings.json")
				if err := os.MkdirAll(filepath.Dir(foldedPeer), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(foldedPeer, []byte("distinct lowercase peer\n"), 0o660); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(foldedPeer, 0o660); err != nil {
					t.Fatal(err)
				}
			}
			if state == "missing" || state == "retargeted" || state == "unlocatable-fold-missing" {
				if err := os.Remove(physical); err != nil {
					t.Fatal(err)
				}
			}
			if state == "retargeted" {
				if err := os.Symlink(literal, physical); err != nil {
					t.Fatal(err)
				}
			}
			if state == "ordinary-retargeted" {
				if err := os.Remove(ordinaryAlias); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(literal, ordinaryAlias); err != nil {
					t.Fatal(err)
				}
			}
			prepared, found, err := Prepare(t.Context(), fixture.Paths, physicalwork.NewCaptureBudget(nil))
			if err != nil || !found {
				t.Fatal("pending physical MCP protection was not discovered", err)
			}
			result, err := Execute(t.Context(), prepared)
			if state == "retargeted" || state == "ordinary-retargeted" {
				if err == nil || result.Complete() {
					t.Fatal("permission recovery admitted an unqualified physical point")
				}
				actual, readErr := os.ReadFile(fixture.Paths.OwnershipRegistryPath)
				if readErr != nil || string(actual) != string(content) {
					t.Fatal("refusal changed the pending Registry", readErr)
				}
				if state != "retargeted" {
					if info, err := os.Stat(physical); err != nil || info.Mode().Perm() != 0o644 {
						t.Fatal("refusal changed the physical point's permissions", err)
					}
				}
			} else {
				if err != nil || !result.Complete() {
					t.Fatal("physical MCP protection did not settle", err)
				}
				_, found, err := Prepare(t.Context(), fixture.Paths, physicalwork.NewCaptureBudget(nil))
				if err != nil || found {
					t.Fatal("settled MCP protection remained discoverable", err)
				}
				info, err := os.Stat(physical)
				if state == "missing" || state == "unlocatable-fold-missing" {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatal("permission recovery recreated the missing physical point", err)
					}
				} else {
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatal("recorded physical permission ceiling was not restored", err)
					}
					if actual, err := os.ReadFile(physical); err != nil || string(actual) != "malformed host payload\n" {
						t.Fatal("permission recovery changed physical content", err)
					}
				}
				if actual, err := fixture.Store.LoadProtectionRegistry(t.Context()); err != nil || len(actual.DocumentBindings()) != 1 || !actual.DocumentBindings()[0].Equal(binding) {
					t.Fatal("permission recovery changed the successful binding", err)
				}
			}
			if info, err := os.Stat(literal); err != nil || info.Mode().Perm() != 0o660 {
				t.Fatal("permission recovery changed the literal MCP path", err)
			}
			if actual, err := os.ReadFile(literal); err != nil || string(actual) != "literal foreign content\n" {
				t.Fatal("permission recovery changed foreign content", err)
			}
			if foldedPeer != "" {
				if info, err := os.Stat(foldedPeer); err != nil || info.Mode().Perm() != 0o660 {
					t.Fatal("permission recovery changed the distinct lowercase peer", err)
				}
				if actual, err := os.ReadFile(foldedPeer); err != nil || string(actual) != "distinct lowercase peer\n" {
					t.Fatal("permission recovery changed distinct peer content", err)
				}
			}
		})
	}
}
