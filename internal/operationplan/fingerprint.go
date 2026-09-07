package operationplan

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// RootIdentity is the project-root witness shared by apply and refresh
// authority fingerprints.
type RootIdentity struct {
	PhysicalRoot         string
	AuthorityFingerprint string
}

type applyRefreshFactDTO struct {
	Kind        string
	Path        string
	Access      mutation.AccessMode
	Effect      mutation.PathEffect
	Target      string
	Scope       string
	Family      string
	Containment mutation.RouteContainment
}

type recoverFactDTO struct {
	Kind     string
	Path     string
	Access   mutation.AccessMode
	Effect   mutation.PathEffect
	Target   string
	Scope    string
	Identity string
}

type applyAuthorityDTO struct {
	Domains         []applyRefreshFactDTO
	ProjectRoot     *RootIdentity
	RecoveryBarrier string
}

type refreshAuthorityDTO struct {
	Domains         []applyRefreshFactDTO
	Root            RootIdentity
	RecoveryBarrier string
}

func applyRefreshDTO(facts []Fact) ([]applyRefreshFactDTO, error) {
	out := make([]applyRefreshFactDTO, 0, len(facts))
	for _, fact := range facts {
		switch fact.kind {
		case FactLogical, FactPhysical, FactRoute, FactRecoveryBarrier:
		default:
			return nil, fmt.Errorf("operationplan: apply/refresh cannot marshal fact kind %d", fact.kind)
		}
		kind, err := fact.kind.wireName()
		if err != nil {
			return nil, err
		}
		out = append(out, applyRefreshFactDTO{
			Kind:        kind,
			Path:        fact.path,
			Access:      fact.access,
			Effect:      fact.effect,
			Target:      fact.target,
			Scope:       fact.scope,
			Family:      fact.family,
			Containment: fact.containment,
		})
	}
	return out, nil
}

func recoverDTO(facts []Fact) ([]recoverFactDTO, error) {
	out := make([]recoverFactDTO, 0, len(facts))
	for _, fact := range facts {
		switch fact.kind {
		case FactLogical, FactPhysical, FactRecoveryRootIdentity, FactStateDirIdentity,
			FactRemovalContinuation, FactRemovalParentIdentity:
		default:
			return nil, fmt.Errorf("operationplan: recover cannot marshal fact kind %d", fact.kind)
		}
		kind, err := fact.kind.wireName()
		if err != nil {
			return nil, err
		}
		out = append(out, recoverFactDTO{
			Kind:     kind,
			Path:     fact.path,
			Access:   fact.access,
			Effect:   fact.effect,
			Target:   fact.target,
			Scope:    fact.scope,
			Identity: fact.identity,
		})
	}
	return out, nil
}

func sortedCopy(facts []Fact, key func(Fact) (string, error)) ([]Fact, error) {
	out := append([]Fact(nil), facts...)
	var keyErr error
	sort.SliceStable(out, func(i, j int) bool {
		if keyErr != nil {
			return false
		}
		left, err := key(out[i])
		if err != nil {
			keyErr = err
			return false
		}
		right, err := key(out[j])
		if err != nil {
			keyErr = err
			return false
		}
		return left < right
	})
	if keyErr != nil {
		return nil, keyErr
	}
	return out, nil
}

// ApplyAuthorityFingerprint is the exact apply authority fingerprint.
func ApplyAuthorityFingerprint(plan Plan, projectRoot *RootIdentity, recoveryBarrier string) (mutation.OperationFingerprint, error) {
	sorted, err := sortedCopy(plan.facts, applyRefreshSortKey)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	facts, err := applyRefreshDTO(sorted)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	payload, err := json.Marshal(applyAuthorityDTO{
		Domains:         facts,
		ProjectRoot:     projectRoot,
		RecoveryBarrier: recoveryBarrier,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint apply authority: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}

// RefreshAuthorityFingerprint is the exact refresh authority fingerprint.
func RefreshAuthorityFingerprint(plan Plan, root RootIdentity, recoveryBarrier string) (mutation.OperationFingerprint, error) {
	sorted, err := sortedCopy(plan.facts, applyRefreshSortKey)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	facts, err := applyRefreshDTO(sorted)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	payload, err := json.Marshal(refreshAuthorityDTO{
		Domains:         facts,
		Root:            root,
		RecoveryBarrier: recoveryBarrier,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint refresh authority: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}

// RecoverAuthorityFingerprint is the exact recover authority fingerprint.
func RecoverAuthorityFingerprint(plan Plan) (mutation.OperationFingerprint, error) {
	sorted, err := sortedCopy(plan.facts, recoverSortKey)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	facts, err := recoverDTO(sorted)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	payload, err := json.Marshal(facts)
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery authority: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}

// HashCanonical hashes already-normalized operation-fingerprint payload bytes.
func HashCanonical(payload []byte) mutation.OperationFingerprint {
	return mutation.NewOperationFingerprint(payload)
}

// HashJSON marshals value with encoding/json and hashes the exact bytes.
func HashJSON(value any) (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	return mutation.NewOperationFingerprint(payload), nil
}
