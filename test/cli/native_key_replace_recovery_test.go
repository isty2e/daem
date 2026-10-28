package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func nativeReplacementFingerprint(t *testing.T, fixture nativeRecoveryFixture, selectors ...string) string {
	t.Helper()
	args := []string{"replace", "host-config", "preferences", "--dry-run", "--json"}
	for _, selector := range selectors {
		args = append(args, "--key", selector)
	}
	_, stdout, _ := runNativeRecoveryCommand(t, fixture, "none", args...)
	var result struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || !strings.HasPrefix(result.Fingerprint, "v1:") {
		t.Fatalf("replacement preview did not emit approval: %s %v", stdout, err)
	}
	return result.Fingerprint
}

func nativeReplacementRecoveryFixture(t *testing.T, kind string) nativeRecoveryFixture {
	t.Helper()
	base := "existing"
	if kind == "unmanaged" {
		base = "absent"
	}
	fixture := newNativeRecoveryFixture(t, base)
	if kind == "same-baseline" {
		writeNativeRecoveryDesired(t, fixture, "before-model", "old.js")
		runNativeRecoveryCommand(t, fixture, "none", "lock")
	}
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), "model='external-model'\n"+nativeRecoveryOutside)
	if err := os.Chmod(fixture.destination, 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestNativeKeyReplacementRecoversActualOriginalValue(t *testing.T) {
	for _, kind := range []string{"unmanaged", "managed", "same-baseline"} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(kind+"/"+boundary, func(t *testing.T) {
				fixture := nativeReplacementRecoveryFixture(t, kind)
				fingerprint := nativeReplacementFingerprint(t, fixture)
				before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
				code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
				if code != nativeRecoveryExit || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
					t.Fatalf("replacement did not reach confirmed %s publication: %d %s %s", boundary, code, stdout, stderr)
				}
				if boundary == "state" && !strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) {
					t.Fatal("missing confirmed state publication", stderr)
				}
				prependNativeRecoveryUserEdit(t, fixture)
				image := snapshotNativeRecoveryPaths(t, fixture.destination)
				identity, err := os.Stat(fixture.destination)
				if err != nil {
					t.Fatal(err)
				}
				runNativeRecoveryCommand(t, fixture, "none", "recover", "--dry-run", "--json")
				runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
				content := testkit.ReadFile(t, fixture.destination)
				if boundary == "host" {
					requireNativeSharedValues(t, configdocument.TOML, content, "model='external-model'\n", true)
					if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
						t.Fatal("rollback changed the actual Before state")
					}
				} else {
					after, err := os.Stat(fixture.destination)
					if err != nil || !os.SameFile(identity, after) || !reflect.DeepEqual(image, snapshotNativeRecoveryPaths(t, fixture.destination)) {
						t.Fatal("After finalization replayed the host write", err)
					}
					_, status, _ := runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
					requireNativeLifecycleClean(t, status)
				}
				requireNativeRecoveryLaterEdit(t, fixture)
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			})
		}
	}
}

func TestNativeKeyReplacementCompensatesReturnedFailures(t *testing.T) {
	for _, boundary := range []string{"return-error", "cancel"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := nativeReplacementRecoveryFixture(t, "same-baseline")
			fingerprint := nativeReplacementFingerprint(t, fixture)
			before := snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			if code == 0 || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
				t.Fatalf("returned failure did not follow a real publication: %d %s %s", code, stdout, stderr)
			}
			requireNativeSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.destination), "model='external-model'\n", true)
			if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.paths.StatefilePath)) {
				t.Fatal("compensation changed durable Before")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
		})
	}
}

func TestNativeKeyReplacementRecoveryRefusesThirdValue(t *testing.T) {
	fixture := nativeReplacementRecoveryFixture(t, "same-baseline")
	fingerprint := nativeReplacementFingerprint(t, fixture)
	if code, _, _ := invokeNativeRecoveryCommand(t, fixture, "host", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint); code != nativeRecoveryExit {
		t.Fatal("replacement was not interrupted after publication", code)
	}
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), "model='third-value'\n"+nativeRecoveryOutside)
	before := snapshotNativeRecoveryEvidence(t, fixture)
	for _, mode := range []string{"--dry-run", "--yes"} {
		if code, _, _ := invokeNativeRecoveryCommand(t, fixture, "none", "recover", mode); code == 0 {
			t.Fatal("recovery overwrote an unrecognized selected value")
		}
		if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture)) {
			t.Fatal("refused recovery changed evidence")
		}
	}
}
