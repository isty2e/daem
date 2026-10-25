package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// These DTOs preserve binding intent evidence, not registry reservation or live
// path authority. Their containing journal owns strict JSON envelope admission.
type recoveryDocumentBindingIntent struct {
	StatefileAuthority pathAuthorityDTO             `json:"statefile_authority"`
	ManifestPath       string                       `json:"manifest_path"`
	Destination        string                       `json:"destination"`
	Before             recoveryDocumentBindingPhase `json:"before"`
	After              recoveryDocumentBindingPhase `json:"after"`
}

type recoveryDocumentBindingPhase struct {
	Kind               string            `json:"kind"`
	Authority          *pathAuthorityDTO `json:"authority,omitempty"`
	NamespaceAuthority *pathAuthorityDTO `json:"namespace_authority,omitempty"`
}

func (record *recoveryDocumentBindingIntent) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("recovery document binding intent destination is nil")
	}
	if err := requireDocumentEvidenceFields(content,
		"statefile_authority", "manifest_path", "destination", "before", "after"); err != nil {
		return err
	}
	type wire recoveryDocumentBindingIntent
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryDocumentBindingIntent(decoded)
	return nil
}

func (phase *recoveryDocumentBindingPhase) UnmarshalJSON(content []byte) error {
	if phase == nil {
		return fmt.Errorf("recovery document binding phase destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "kind"); err != nil {
		return err
	}
	type wire recoveryDocumentBindingPhase
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*phase = recoveryDocumentBindingPhase(decoded)
	return nil
}

func persistedDocumentBindingIntent(intent ownership.DocumentBindingIntent) (recoveryDocumentBindingIntent, error) {
	if err := intent.Validate(); err != nil {
		return recoveryDocumentBindingIntent{}, err
	}
	record := recoveryDocumentBindingIntent{
		StatefileAuthority: *persistedPathAuthority(intent.Owner().StatefileAuthority()),
		ManifestPath:       intent.Owner().ManifestPath(),
		Destination:        intent.Destination().String(),
		Before:             persistedDocumentBindingValue(intent.Before()),
	}
	if after, known := intent.KnownAfter(); known {
		record.After = persistedDocumentBindingValue(after)
	} else {
		provisional, _ := intent.ProvisionalAfter()
		record.After = recoveryDocumentBindingPhase{
			Kind:               "provisional",
			Authority:          &pathAuthorityDTO{Key: provisional.CandidateKey(), Witness: provisional.CandidateWitness()},
			NamespaceAuthority: persistedPathAuthority(provisional.Namespace()),
		}
	}
	return record, nil
}

func persistedDocumentBindingValue(value ownership.DocumentBindingValue) recoveryDocumentBindingPhase {
	if binding, present := value.Get(); present {
		return recoveryDocumentBindingPhase{Kind: "exact", Authority: persistedPathAuthority(binding.PathAuthority())}
	}
	return recoveryDocumentBindingPhase{Kind: "absent"}
}

func (record recoveryDocumentBindingIntent) canonical() (ownership.DocumentBindingIntent, error) {
	statefile, err := canonicalPathAuthority(record.StatefileAuthority)
	if err != nil {
		return ownership.DocumentBindingIntent{}, fmt.Errorf("document binding statefile authority: %w", err)
	}
	owner, err := stateauthority.New(statefile, record.ManifestPath)
	if err != nil {
		return ownership.DocumentBindingIntent{}, err
	}
	destination, err := output.Parse(record.Destination)
	if err != nil {
		return ownership.DocumentBindingIntent{}, err
	}
	before, err := record.Before.canonicalValue(owner, destination)
	if err != nil {
		return ownership.DocumentBindingIntent{}, fmt.Errorf("document binding before: %w", err)
	}
	if record.After.Kind == "provisional" {
		if record.After.Authority == nil || record.After.NamespaceAuthority == nil {
			return ownership.DocumentBindingIntent{}, fmt.Errorf("provisional document binding requires candidate and namespace authorities")
		}
		if _, present := before.Get(); present {
			return ownership.DocumentBindingIntent{}, fmt.Errorf("provisional document binding requires absent before")
		}
		provisional, err := pathauthority.NewProvisional(
			record.After.Authority.Key, record.After.Authority.Witness,
			record.After.NamespaceAuthority.Key, record.After.NamespaceAuthority.Witness,
		)
		if err != nil {
			return ownership.DocumentBindingIntent{}, err
		}
		return ownership.NewProvisionalDocumentBindingIntent(owner, destination, provisional)
	}
	after, err := record.After.canonicalValue(owner, destination)
	if err != nil {
		return ownership.DocumentBindingIntent{}, fmt.Errorf("document binding after: %w", err)
	}
	return ownership.NewExactDocumentBindingIntent(owner, destination, before, after)
}

func (phase recoveryDocumentBindingPhase) canonicalValue(owner stateauthority.Authority, destination output.Destination) (ownership.DocumentBindingValue, error) {
	if phase.NamespaceAuthority != nil {
		return ownership.NoDocumentBinding(), fmt.Errorf("exact or absent document binding cannot carry provisional namespace evidence")
	}
	switch phase.Kind {
	case "absent":
		if phase.Authority != nil {
			return ownership.NoDocumentBinding(), fmt.Errorf("absent document binding cannot carry path authority")
		}
		return ownership.NoDocumentBinding(), nil
	case "exact":
		if phase.Authority == nil {
			return ownership.NoDocumentBinding(), fmt.Errorf("exact document binding requires path authority")
		}
		exact, err := canonicalPathAuthority(*phase.Authority)
		if err != nil {
			return ownership.NoDocumentBinding(), err
		}
		binding, err := ownership.NewDocumentBinding(owner, destination, exact)
		if err != nil {
			return ownership.NoDocumentBinding(), err
		}
		return ownership.PresentDocumentBinding(binding)
	default:
		return ownership.NoDocumentBinding(), fmt.Errorf("unsupported exact document binding phase %q", phase.Kind)
	}
}
