package journal

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func TestRecoverySubjectsRemainDisjointCorrelationKeys(t *testing.T) {
	first := recoveryStateKey{
		subject: mustTestManagedPathSubject("project", "instructions.project.claude"),
		scope:   string(target.ScopeProject),
		path:    "AGENTS.md",
	}
	second := first
	second.subject = mustTestManagedPathSubject("project", "instructions.project.codex")

	state := map[recoveryStateKey]string{
		first:  "first",
		second: "second",
	}
	if len(state) != 2 || state[first] != "first" || state[second] != "second" {
		t.Fatalf("subjects collapsed in correlation map: %#v", state)
	}
}

func TestLoadActivePlanRequiresResolverForRootedAuthority(t *testing.T) {
	paths := Paths{RecoveryDir: filepath.Join(t.TempDir(), "recovery")}
	resolver := func(output.Destination) (string, error) {
		return "/unused", nil
	}
	capability := func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		return nil, false, nil
	}
	if _, err := LoadActivePlanWithOptions(
		context.Background(),
		paths,
		PlanLoadOptions{Filesystem: journalTestFilesystem(), Resolver: resolver},
	); err == nil || !strings.Contains(err.Error(), "no active recovery journal") {
		t.Fatalf("resolver-only LoadActivePlanWithOptions error = %v, want ordinary plan lookup", err)
	}
	_, err := LoadActivePlanWithOptions(
		context.Background(),
		paths,
		PlanLoadOptions{RootedCapability: capability},
	)
	if err == nil || !strings.Contains(err.Error(), "requires a destination resolver") {
		t.Fatalf("rooted-only LoadActivePlanWithOptions error = %v, want resolver requirement", err)
	}
}

func TestLoadActivePlanRequiresFilesystemBeforeRecoveryObservation(t *testing.T) {
	_, err := LoadActivePlanWithOptions(
		context.Background(),
		Paths{},
		PlanLoadOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "filesystem is required") {
		t.Fatalf("LoadActivePlanWithOptions error = %v, want filesystem requirement", err)
	}
}

func TestRecoveryPlanJournalAuthorityFingerprintIncludesCompleteDurableEvidence(t *testing.T) {
	original := mustBuildRecoveryPlan(t, defaultRecoveryJournal(), beforeStatefile(), []recoveryPathObservation{
		beforePathObservation(defaultRecoveryEntry()),
	}, nil)
	originalFingerprint, err := original.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*recoveryJournal)
	}{
		{
			name: "statefile after",
			mutate: func(candidate *recoveryJournal) {
				subject := mustTestManagedPathSubject(
					"authority-drift",
					"instructions.project.agents",
				)
				attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
					Subject:         subject,
					Target:          target.TargetCodex,
					Scope:           target.ScopeProject,
					PlanIdentityKey: "delegate:test-authority-drift",
					ObservedAt:      time.Date(2026, time.July, 17, 0, 0, 0, 0, time.UTC),
					Status:          durableattempt.DelegateStatusSucceeded,
					Reason:          durableattempt.DelegateReasonNone,
				})
				if err != nil {
					panic(err)
				}
				candidate.StatefileAfter, err = candidate.StatefileAfter.WithDelegateAttempts(
					[]durableattempt.DelegateAttempt{attempt},
				)
				if err != nil {
					panic(err)
				}
			},
		},
		{
			name: "manifest root provenance",
			mutate: func(candidate *recoveryJournal) {
				provenance := candidate.ManifestRootProvenance
				provenance.ObjectFingerprint = "sha256:" + strings.Repeat("3", 64)
				candidate.ManifestRootProvenance = provenance
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := defaultRecoveryJournal()
			test.mutate(&candidate)
			otherFingerprint, err := recoveryJournalAuthorityFingerprint(
				candidate,
				testStateCodec(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if otherFingerprint == originalFingerprint {
				t.Fatalf("different %s retained journal authority fingerprint", test.name)
			}
		})
	}
}

func TestSelectedRecoveryPlanRetainsCompleteJournalAuthorityFingerprint(t *testing.T) {
	first := defaultRecoveryEntry()
	second := recoveryEntryFor(
		"second",
		"CLAUDE.md",
		"sha256:second-before",
		"sha256:second-after",
		"backups/CLAUDE.md",
	)
	journal := recoveryJournalFor(first, second)
	full := mustBuildRecoveryPlan(
		t,
		journal,
		journal.StatefileBefore,
		[]recoveryPathObservation{
			beforePathObservation(first),
			beforePathObservation(second),
		},
		nil,
	)
	fullFingerprint, err := full.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	selected, err := buildRecoveryPlanForEntries(
		testOperationID,
		testOperationDir,
		journal,
		[]recoveryEntry{first},
		journal.StatefileBefore,
		[]recoveryPathObservation{beforePathObservation(first)},
		nil,
		nil,
		ownership.EmptyRegistry(),
		fullFingerprint,
	)
	if err != nil {
		t.Fatalf("build selected recovery plan: %v", err)
	}

	selectedFingerprint, err := selected.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if selectedFingerprint != fullFingerprint {
		t.Fatalf(
			"selected fingerprint = %q, want complete journal fingerprint %q",
			selectedFingerprint,
			fullFingerprint,
		)
	}
	if guarded := selected.GuardedActions(); len(guarded) != 1 || guarded[0].Destination != first.Path {
		t.Fatalf("selected guarded actions = %#v, want only %q", guarded, first.Path)
	}
}

func TestRecoveryJournalRejectsUnadmittedManagedPathOccupancy(t *testing.T) {
	t.Parallel()

	for _, mutate := range []func(*recoveryEntry){
		func(entry *recoveryEntry) { entry.Subject.Name = "hook:oracle" },
		func(entry *recoveryEntry) { entry.ContentKind = "file" },
		func(entry *recoveryEntry) { entry.ContentPath = "/nested" },
		func(entry *recoveryEntry) { entry.Path = "../escape" },
		func(entry *recoveryEntry) { entry.Path = "~/.agents/skills/oracle" },
		func(entry *recoveryEntry) {
			entry.Subject.Namespace = "skill.project.claude"
			entry.Path = ".claude/skills/oracle"
		},
	} {
		entry := managedPathRecoveryEntry()
		mutate(&entry)
		if err := validateRecoveryStateIdentity(recoveryStateIdentityFromEntry(entry)); err == nil {
			t.Fatalf("validateRecoveryStateIdentity accepted %#v", entry)
		}
	}
}
