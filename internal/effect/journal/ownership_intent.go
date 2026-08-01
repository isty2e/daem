package journal

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

const (
	recoveryProvisionalAcquireKind    = "provisional_acquire"
	recoveryProvisionalAcquireVersion = 1
)

type recoveryProvisionalAcquireIntent struct {
	Kind               string           `json:"kind"`
	Version            int              `json:"version"`
	Destination        string           `json:"destination"`
	ContentPath        string           `json:"content_path,omitempty"`
	CandidateAuthority pathAuthorityDTO `json:"candidate_authority"`
	NamespaceAuthority pathAuthorityDTO `json:"namespace_authority"`
	StatefileAuthority pathAuthorityDTO `json:"statefile_authority"`
	ManifestPath       string           `json:"manifest_path"`
	OperationID        string           `json:"operation_id"`
}

func recoveryProvisionalAcquireIntents(
	intents []outputownership.ProvisionalAcquireIntent,
) ([]recoveryProvisionalAcquireIntent, error) {
	persisted := make([]recoveryProvisionalAcquireIntent, 0, len(intents))
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return nil, fmt.Errorf("provisional acquire intent[%d]: %w", index, err)
		}
		path := intent.Path()
		namespace := path.Namespace()
		statefile := intent.Owner().StatefileAuthority()
		persisted = append(persisted, recoveryProvisionalAcquireIntent{
			Kind:        recoveryProvisionalAcquireKind,
			Version:     recoveryProvisionalAcquireVersion,
			Destination: intent.Destination().String(),
			ContentPath: string(intent.ContentPath()),
			CandidateAuthority: pathAuthorityDTO{
				Key: path.CandidateKey(), Witness: path.CandidateWitness(),
			},
			NamespaceAuthority: pathAuthorityDTO{
				Key: namespace.Key(), Witness: namespace.Witness(),
			},
			StatefileAuthority: pathAuthorityDTO{
				Key: statefile.Key(), Witness: statefile.Witness(),
			},
			ManifestPath: intent.Owner().ManifestPath(),
			OperationID:  intent.OperationID(),
		})
	}
	sort.Slice(persisted, func(left int, right int) bool {
		if persisted[left].Destination != persisted[right].Destination {
			return persisted[left].Destination < persisted[right].Destination
		}
		return persisted[left].ContentPath < persisted[right].ContentPath
	})
	return persisted, nil
}

func canonicalProvisionalAcquireIntents(
	persisted []recoveryProvisionalAcquireIntent,
) ([]outputownership.ProvisionalAcquireIntent, error) {
	intents := make([]outputownership.ProvisionalAcquireIntent, 0, len(persisted))
	for index, record := range persisted {
		if record.Kind != recoveryProvisionalAcquireKind {
			return nil, fmt.Errorf(
				"recovery provisional_acquire_intents[%d] has unsupported kind %q",
				index,
				record.Kind,
			)
		}
		if record.Version != recoveryProvisionalAcquireVersion {
			return nil, fmt.Errorf(
				"recovery provisional_acquire_intents[%d] has unsupported version %d",
				index,
				record.Version,
			)
		}
		destination, err := output.Parse(record.Destination)
		if err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d].destination: %w", index, err)
		}
		if err := destination.ValidateScope(target.ScopeGlobal); err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d].destination: %w", index, err)
		}
		provisional, err := pathauthority.NewProvisional(
			record.CandidateAuthority.Key,
			record.CandidateAuthority.Witness,
			record.NamespaceAuthority.Key,
			record.NamespaceAuthority.Witness,
		)
		if err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d].path: %w", index, err)
		}
		if _, err := pathauthority.NewExact(
			record.NamespaceAuthority.Key,
			record.NamespaceAuthority.Witness,
		); err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d].namespace_authority: %w", index, err)
		}
		statefile, err := pathauthority.NewExact(
			record.StatefileAuthority.Key,
			record.StatefileAuthority.Witness,
		)
		if err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d].statefile_authority: %w", index, err)
		}
		owner, err := stateauthority.New(statefile, record.ManifestPath)
		if err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d].owner: %w", index, err)
		}
		intent, err := outputownership.NewProvisionalAcquireIntent(
			destination,
			output.ContentPath(record.ContentPath),
			provisional,
			owner,
			record.OperationID,
		)
		if err != nil {
			return nil, fmt.Errorf("recovery provisional_acquire_intents[%d]: %w", index, err)
		}
		intents = append(intents, intent)
	}
	if err := validateNonOverlappingProvisionalAcquireIntents(intents); err != nil {
		return nil, err
	}
	return intents, nil
}

func validateNonOverlappingProvisionalAcquireIntents(
	intents []outputownership.ProvisionalAcquireIntent,
) error {
	type intentFootprint struct {
		destination      string
		candidateKey     string
		candidateWitness string
		namespaceKey     string
		namespaceWitness string
	}
	groups := make(map[intentFootprint][]string, len(intents))
	for _, intent := range intents {
		path := intent.Path()
		namespace := path.Namespace()
		key := intentFootprint{
			destination:      intent.Destination().String(),
			candidateKey:     path.CandidateKey(),
			candidateWitness: path.CandidateWitness(),
			namespaceKey:     namespace.Key(),
			namespaceWitness: namespace.Witness(),
		}
		groups[key] = append(groups[key], string(intent.ContentPath()))
	}
	for _, contentPaths := range groups {
		if err := validateNonOverlappingRecoveryContentPaths(contentPaths); err != nil {
			return fmt.Errorf("recovery provisional acquire intents contain overlapping footprints")
		}
	}
	return nil
}

func validateRecoveryIntentAuthorities(
	intents []outputownership.ProvisionalAcquireIntent,
	authority mutation.PersistedDirectoryEntryAuthority,
) error {
	for index, intent := range intents {
		if !authority.Exact().Equal(intent.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"recovery provisional_acquire_intents[%d] has incompatible state authority %q with semantics %q",
				index,
				intent.Owner().StatefileKey(),
				intent.Owner().StatefileAuthority().Witness(),
			)
		}
	}
	return nil
}

func provisionalAcquireIntentKey(destination output.Destination, contentPath output.ContentPath) string {
	return destination.String() + "\x00" + string(contentPath)
}
