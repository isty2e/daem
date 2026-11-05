package record

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
)

func TestClaimCoverageSessionRejectsIncompleteAndRepeatedSupply(t *testing.T) {
	for _, action := range []string{"zero", "unfinished", "pending observation", "invalid observation", "double request", "double supply", "canceled"} {
		t.Run(action, func(t *testing.T) {
			entry := globalAcquireRecoveryEntry(t)
			transition, _, _ := testAcquireTransition(t, entry)
			session, err := newClaimCoverageSession([]recoveryEntry{entry}, []ownershipmutation.ClaimTransition{transition}, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "zero":
				session = &claimCoverageSession{}
				_, _, err = session.next(t.Context())
			case "unfinished":
				_, err = session.finish()
			case "pending observation", "invalid observation":
				if _, present, demandErr := session.next(t.Context()); demandErr != nil || !present {
					t.Fatalf("missing first demand: %v", demandErr)
				}
				if action == "pending observation" {
					_, err = session.finish()
				} else {
					err = session.bind(mutation.DirectoryEntryAuthorityObservation{})
				}
			case "double request":
				if _, present, demandErr := session.next(t.Context()); demandErr != nil || !present {
					t.Fatalf("missing first demand: %v", demandErr)
				}
				_, _, err = session.next(t.Context())
			case "double supply":
				if _, present, demandErr := session.next(t.Context()); demandErr != nil || !present {
					t.Fatalf("missing first demand: %v", demandErr)
				}
				observation, observeErr := mutation.ObserveDirectoryEntryAuthority("/tmp/daem-global-config.json")
				if observeErr != nil {
					t.Fatal(observeErr)
				}
				if bindErr := session.bind(observation); bindErr != nil {
					t.Fatal(bindErr)
				}
				err = session.bind(observation)
			case "canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, _, err = session.next(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			}
			if err == nil {
				t.Fatal("invalid coverage phase was accepted")
			}
			if result, err := session.finish(); err == nil || result != nil {
				t.Fatal("failed session returned usable entry coverage")
			}
		})
	}
}

func TestClaimCoverageSessionFinishesOnlyCompleteIsolatedEvidence(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	transition, _, _ := testAcquireTransition(t, entry)
	transitions := []ownershipmutation.ClaimTransition{transition}
	session, err := newClaimCoverageSession([]recoveryEntry{entry}, transitions, nil)
	if err != nil {
		t.Fatal(err)
	}
	transitions[0] = ownershipmutation.ClaimTransition{}
	demand, present, err := session.next(t.Context())
	if err != nil || !present || demand.index != 0 {
		t.Fatalf("coverage lost its first demand: %v", err)
	}
	observation, err := mutation.ObserveDirectoryEntryAuthority("/tmp/daem-global-config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.bind(observation); err != nil {
		t.Fatal(err)
	}
	if _, present, err := session.next(t.Context()); err != nil || present {
		t.Fatalf("coverage did not close: %v", err)
	}
	result, err := session.finish()
	if err != nil || len(result) != 1 || result[0].OwnershipPathAuthority == nil {
		t.Fatalf("complete coverage was lost: %v", err)
	}
	if err := validateRecoveryClaimCoverage(result, []ownershipmutation.ClaimTransition{transition}, nil); err != nil {
		t.Fatal(err)
	}
	if entry.OwnershipPathAuthority != nil {
		t.Fatal("coverage mutated its input record entry")
	}
	if result, err := session.finish(); err == nil || result != nil {
		t.Fatal("finished coverage was reused")
	}
}
