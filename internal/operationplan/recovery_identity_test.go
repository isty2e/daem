package operationplan

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
)

func TestActiveRecoveryOperationFingerprintMatchesOwnerProjection(t *testing.T) {
	t.Parallel()

	input := ActiveRecoveryIdentityInput{
		ManifestRoot:                 "/workspace",
		StatefilePath:                "/workspace/.daem/state.toml",
		RecoveryDir:                  "/workspace/.daem/recovery",
		OperationID:                  "operation-1",
		OperationDir:                 "/workspace/.daem/recovery/operation-1",
		Classification:               "needs_rollback",
		JournalAuthorityFingerprint:  "journal-fingerprint",
		StateDirAuthorityFingerprint: "state-dir-fingerprint",
		Actions:                      json.RawMessage(`[{"Kind":"restore_write","Destination":{}}]`),
		GuardedActions:               json.RawMessage(`null`),
		RemovalCleanupObligations: []ActiveRecoveryCleanupObligation{{
			Scope:       "project",
			Destination: json.RawMessage(`{}`),
			Action:      "remove_residue",
			Readiness:   "ready",
			Reason:      "none",
			Detail:      "detail",
		}},
		StatefileBefore: json.RawMessage(`{"version":1}`),
		ClaimTransitions: []ActiveRecoveryClaimTransition{{
			Kind: "retain",
			PathAuthority: RecoveryPathAuthorityFingerprint{
				Key: "path-key", SemanticsWitness: "path-witness",
			},
			Footprint: footprintcodec.Record{Kind: "path"},
			OwnerStatefileAuthority: RecoveryPathAuthorityFingerprint{
				Key: "owner-key", SemanticsWitness: "owner-witness",
			},
			OwnerManifestPath: "/workspace/daem.toml",
			OperationID:       "operation-1",
		}},
	}

	compiled, err := ActiveRecoveryOperationFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	ownerPayload, err := json.Marshal(struct {
		ManifestRoot                 string
		StatefilePath                string
		RecoveryDir                  string
		OperationID                  string
		OperationDir                 string
		Classification               string
		JournalAuthorityFingerprint  string
		StateDirAuthorityFingerprint string
		Actions                      json.RawMessage
		GuardedActions               json.RawMessage
		RemovalCleanupObligations    []ActiveRecoveryCleanupObligation
		StatefileBefore              json.RawMessage
		ClaimTransitions             []ActiveRecoveryClaimTransition
	}{
		ManifestRoot:                 input.ManifestRoot,
		StatefilePath:                input.StatefilePath,
		RecoveryDir:                  input.RecoveryDir,
		OperationID:                  input.OperationID,
		OperationDir:                 input.OperationDir,
		Classification:               input.Classification,
		JournalAuthorityFingerprint:  input.JournalAuthorityFingerprint,
		StateDirAuthorityFingerprint: input.StateDirAuthorityFingerprint,
		Actions:                      input.Actions,
		GuardedActions:               input.GuardedActions,
		RemovalCleanupObligations:    input.RemovalCleanupObligations,
		StatefileBefore:              input.StatefileBefore,
		ClaimTransitions:             input.ClaimTransitions,
	})
	if err != nil {
		t.Fatal(err)
	}
	owner := mutation.NewOperationFingerprint(ownerPayload)
	if !compiled.Equal(owner) {
		t.Fatal("compiled active recovery fingerprint differs from the owner projection")
	}
}

func TestActiveRecoveryOperationFingerprintRejectsMalformedOwnerProjection(t *testing.T) {
	t.Parallel()

	_, err := ActiveRecoveryOperationFingerprint(ActiveRecoveryIdentityInput{
		Actions:         json.RawMessage(`{`),
		GuardedActions:  json.RawMessage(`[]`),
		StatefileBefore: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("malformed owner projection was accepted")
	}
}
