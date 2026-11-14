//go:build darwin || linux

package fileset

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationRetirementResolvesObservedNamespaceNotErrorClass(t *testing.T) {
	for _, visible := range []bool{false, true} {
		t.Run(map[bool]string{false: "source retained", true: "server renamed"}[visible], func(t *testing.T) {
			input, revision := interruptedPublicationForTest(t, true)
			plan, err := loadQualifiedPublication(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.rootWitness.Close()
			name := publicationRetiredPrefix + strings.Repeat("a", 32)
			if visible {
				if err := plan.renameEvidence(t.Context(), transactionDirName, name, plan.activeIdentity); err != nil {
					t.Fatal(err)
				}
			}
			transportErr := errors.New("ambiguous rename response")
			err = plan.resolveRetirement(t.Context(), name, plan.activeIdentity, revision, transportErr)
			if visible {
				if err != nil {
					t.Fatal("verified durable retirement did not converge", err)
				}
				assertMissing(t, transactionDir(input.EvidenceRoot))
				if _, err := plan.qualifyEvidenceAt(t.Context(), name, revision, plan.activeIdentity); err != nil {
					t.Fatal("retired evidence was lost", err)
				}
			} else {
				if !errors.Is(err, transportErr) {
					t.Fatal("uncommitted rename was reported as success", err)
				}
				if _, err := plan.qualifyEvidenceAt(t.Context(), transactionDirName, revision, plan.activeIdentity); err != nil {
					t.Fatal("original active evidence not retained", err)
				}
			}
		})
	}
}

func TestPublicationCanRestoreOnlyTheQualifiedRetiredTree(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "do not replace"}[conflict], func(t *testing.T) {
			input, revision := interruptedPublicationForTest(t, true)
			plan, err := loadQualifiedPublication(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.rootWitness.Close()
			name := publicationRetiredPrefix + strings.Repeat("b", 32)
			if err := plan.renameEvidence(t.Context(), transactionDirName, name, plan.activeIdentity); err != nil {
				t.Fatal(err)
			}
			retired, err := plan.qualifyEvidenceAt(t.Context(), name, revision, plan.activeIdentity)
			if err != nil {
				t.Fatal(err)
			}
			canary := filepath.Join(transactionDir(input.EvidenceRoot), "preserve")
			if conflict {
				writeFixture(t, canary, "foreign", 0o600)
			}
			err = plan.restoreActiveEvidence(t.Context(), name, revision, retired)
			if conflict {
				if err == nil {
					t.Fatal("active rebind overwrote a conflicting namespace")
				}
				content, err := os.ReadFile(canary)
				if err != nil || string(content) != "foreign" {
					t.Fatal("conflicting active content changed", err)
				}
				if _, err := plan.qualifyEvidenceAt(t.Context(), name, revision, retired); err != nil {
					t.Fatal("no-replace refusal lost original retired evidence", err)
				}
			} else {
				if err != nil {
					t.Fatal("could not restore the original active fence", err)
				}
				assertMissing(t, filepath.Join(input.EvidenceRoot, name))
				if _, err := plan.qualifyEvidenceAt(t.Context(), transactionDirName, revision, retired); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
