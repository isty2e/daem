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
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func nativeReplacementFingerprint(t *testing.T, fixture nativefixture.RecoveryFixture, selectors ...string) string {
	t.Helper()
	args := []string{"replace", "host-config", "preferences", "--dry-run", "--json"}
	for _, selector := range selectors {
		args = append(args, "--key", selector)
	}
	_, stdout, _ := nativefixture.RunRecoveryCommand(t, fixture, "none", args...)
	var result struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || !strings.HasPrefix(result.Fingerprint, "v1:") {
		t.Fatalf("replacement preview did not emit approval: %s %v", stdout, err)
	}
	return result.Fingerprint
}

func nativeReplacementRecoveryFixture(t *testing.T, kind string) nativefixture.RecoveryFixture {
	t.Helper()
	base := "existing"
	if kind == "unmanaged" {
		base = "absent"
	}
	fixture := nativefixture.NewRecoveryFixture(t, base)
	if kind == "same-baseline" {
		nativefixture.WriteRecoveryDesired(t, fixture, "before-model", "old.js")
		nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
	}
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), "model='external-model'\n"+nativefixture.RecoveryOutside)
	if err := os.Chmod(fixture.Destination, 0o644); err != nil {
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
				before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, boundary, "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
				if code != nativefixture.RecoveryExit || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
					t.Fatalf("replacement did not reach confirmed %s publication: %d %s %s", boundary, code, stdout, stderr)
				}
				if boundary == "state" && !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Paths.StatefilePath)) {
					t.Fatal("missing confirmed state publication", stderr)
				}
				nativefixture.PrependRecoveryUserEdit(t, fixture)
				image := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)
				identity, err := os.Stat(fixture.Destination)
				if err != nil {
					t.Fatal(err)
				}
				nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--dry-run", "--json")
				nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
				content := testkit.ReadFile(t, fixture.Destination)
				if boundary == "host" {
					nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='external-model'\n", true)
					if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
						t.Fatal("rollback changed the actual Before state")
					}
				} else {
					after, err := os.Stat(fixture.Destination)
					if err != nil || !os.SameFile(identity, after) || !reflect.DeepEqual(image, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination)) {
						t.Fatal("After finalization replayed the host write", err)
					}
					_, status, _ := nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
					nativefixture.RequireLifecycleClean(t, status)
				}
				nativefixture.RequireRecoveryLaterEdit(t, fixture)
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			})
		}
	}
}

func TestNativeKeyReplacementCompensatesReturnedFailures(t *testing.T) {
	for _, boundary := range []string{"return-error", "cancel"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := nativeReplacementRecoveryFixture(t, "same-baseline")
			fingerprint := nativeReplacementFingerprint(t, fixture)
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, boundary, "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			if code == 0 || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
				t.Fatalf("returned failure did not follow a real publication: %d %s %s", code, stdout, stderr)
			}
			nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='external-model'\n", true)
			if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Paths.StatefilePath)) {
				t.Fatal("compensation changed durable Before")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
		})
	}
}

func TestNativeKeyReplacementRecoveryRefusesThirdValue(t *testing.T) {
	fixture := nativeReplacementRecoveryFixture(t, "same-baseline")
	fingerprint := nativeReplacementFingerprint(t, fixture)
	if code, _, _ := nativefixture.InvokeRecoveryCommand(t, fixture, "host", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint); code != nativefixture.RecoveryExit {
		t.Fatal("replacement was not interrupted after publication", code)
	}
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), "model='third-value'\n"+nativefixture.RecoveryOutside)
	before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
	for _, mode := range []string{"--dry-run", "--yes"} {
		if code, _, _ := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", mode); code == 0 {
			t.Fatal("recovery overwrote an unrecognized selected value")
		}
		if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
			t.Fatal("refused recovery changed evidence")
		}
	}
}
