// Package extension imports source-exact host extension relations into
// reviewable desired declarations without acquiring lifecycle authority.
package extension

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// Input selects host inventories and supplies fixed existing declarations for
// merge-safe ID and order planning.
type Input struct {
	ManifestRoot string
	Targets      []target.Target
	Scopes       []target.Scope
	Existing     []desiredextension.Extension
}

// Skip is one selected live extension row that cannot reconstruct a desired
// declaration.
type Skip struct {
	LivePath string
	Reason   string
	Target   target.Target
	Scope    target.Scope
}

// Scan records one selected host extension inventory used by the proposal.
// Its path remains boundary evidence and never participates in desired
// extension identity.
type Scan struct {
	LivePath     string
	Target       target.Target
	Scope        target.Scope
	Entries      int
	Imported     int
	Skipped      int
	MaximumBytes int64
}

// Result is one immutable exact-extension import proposal. Extensions contains
// source-exact observed declarations only. Order may also contain fixed
// existing relations needed to position additions without moving old blocks.
type Result struct {
	extensions        []desiredextension.Extension
	orderedExtensions []desiredextension.Extension
	order             []desiredextension.CarrierKey
	sequences         []relationobserve.ObservedRelationSequence
	constraints       []hostrelation.RelationOrderConstraint
	scans             []Scan
	skipped           []Skip
}

// Extensions returns source-exact import candidates in proposed declaration order.
func (result Result) Extensions() []desiredextension.Extension {
	return append([]desiredextension.Extension(nil), result.extensions...)
}

// OrderedExtensions returns the complete ordered declaration proposal,
// including fixed existing relations needed for merge placement.
func (result Result) OrderedExtensions() []desiredextension.Extension {
	return append(
		[]desiredextension.Extension(nil),
		result.orderedExtensions...,
	)
}

// Scans returns selected physical inventory evidence.
func (result Result) Scans() []Scan {
	return append([]Scan(nil), result.scans...)
}

// Skipped returns selected source-inexact or unrepresentable observations.
func (result Result) Skipped() []Skip {
	return append([]Skip(nil), result.skipped...)
}

// IdentityBytes returns a deterministic disclosure of the complete exact
// import proposal, including physical observation evidence and desired order.
func (result Result) IdentityBytes() ([]byte, error) {
	type extensionIdentity struct {
		ID         string
		Carrier    string
		Target     string
		Scope      string
		SourceKind string
		Source     string
	}
	type sequenceRowIdentity struct {
		HostLoadIdentity string
		Subject          string
	}
	type sequenceIdentity struct {
		ClassID    string
		SequenceID string
		Authority  string
		Revision   string
		Rows       []sequenceRowIdentity
	}
	type constraintMemberIdentity struct {
		Subject          string
		HostLoadIdentity string
	}
	type constraintIdentity struct {
		ClassID                string
		MemberIdentityContract string
		RuntimeMeaning         string
		Members                []constraintMemberIdentity
		Fingerprint            string
	}

	projectExtension := func(value desiredextension.Extension) extensionIdentity {
		return extensionIdentity{
			ID:         value.ID().Name(),
			Carrier:    string(value.Carrier()),
			Target:     string(value.Target()),
			Scope:      string(value.Scope()),
			SourceKind: string(value.Source().Kind()),
			Source:     value.Source().Ref(),
		}
	}
	extensions := make([]extensionIdentity, 0, len(result.extensions))
	for _, value := range result.extensions {
		extensions = append(extensions, projectExtension(value))
	}
	orderedExtensions := make([]extensionIdentity, 0, len(result.orderedExtensions))
	for _, value := range result.orderedExtensions {
		orderedExtensions = append(orderedExtensions, projectExtension(value))
	}
	sequences := make([]sequenceIdentity, 0, len(result.sequences))
	for _, sequence := range result.sequences {
		rows := make([]sequenceRowIdentity, 0, len(sequence.OrderedRows()))
		for _, row := range sequence.OrderedRows() {
			subject, correlated := row.CorrelatedSubject()
			subjectValue := ""
			if correlated {
				subjectValue = subject.String()
			}
			rows = append(rows, sequenceRowIdentity{
				HostLoadIdentity: string(row.HostLoadIdentity()),
				Subject:          subjectValue,
			})
		}
		sequences = append(sequences, sequenceIdentity{
			ClassID:    string(sequence.ClassID()),
			SequenceID: string(sequence.SequenceID()),
			Authority:  string(sequence.Authority()),
			Revision:   string(sequence.Revision()),
			Rows:       rows,
		})
	}
	constraints := make([]constraintIdentity, 0, len(result.constraints))
	for _, constraint := range result.constraints {
		members := make([]constraintMemberIdentity, 0, len(constraint.Members()))
		for _, member := range constraint.Members() {
			members = append(members, constraintMemberIdentity{
				Subject:          member.Subject().String(),
				HostLoadIdentity: string(member.HostLoadIdentity()),
			})
		}
		constraints = append(constraints, constraintIdentity{
			ClassID:                string(constraint.ClassID()),
			MemberIdentityContract: constraint.MemberIdentityContract(),
			RuntimeMeaning:         string(constraint.RuntimeMeaning()),
			Members:                members,
			Fingerprint:            constraint.Fingerprint(),
		})
	}

	return json.Marshal(struct {
		Extensions        []extensionIdentity
		OrderedExtensions []extensionIdentity
		Sequences         []sequenceIdentity
		Constraints       []constraintIdentity
		Scans             []Scan
		Skipped           []Skip
	}{
		Extensions:        extensions,
		OrderedExtensions: orderedExtensions,
		Sequences:         sequences,
		Constraints:       constraints,
		Scans:             result.Scans(),
		Skipped:           result.Skipped(),
	})
}

// WithExtensions returns the same evidence/order proposal narrowed to writable
// imported declarations. Every retained relation must belong to the original
// exact-import result.
func (result Result) WithExtensions(
	values []desiredextension.Extension,
) (Result, error) {
	selected := make(map[desiredextension.CarrierKey]struct{}, len(values))
	available := make(map[desiredextension.CarrierKey]struct{}, len(result.extensions))
	for _, value := range result.extensions {
		available[value.CarrierKey()] = struct{}{}
	}
	for index, value := range values {
		if err := value.Validate(); err != nil {
			return Result{}, fmt.Errorf("extension candidate %d: %w", index, err)
		}
		key := value.CarrierKey()
		if _, ok := available[key]; !ok {
			return Result{}, fmt.Errorf(
				"extension candidate %d is outside the exact-import proposal",
				index,
			)
		}
		if _, duplicate := selected[key]; duplicate {
			return Result{}, fmt.Errorf(
				"extension candidate %d duplicates one relation",
				index,
			)
		}
		selected[key] = struct{}{}
	}
	filtered := make([]desiredextension.Extension, 0, len(selected))
	for _, value := range result.extensions {
		if _, keep := selected[value.CarrierKey()]; keep {
			filtered = append(filtered, value)
		}
	}
	result.extensions = filtered
	return result, nil
}

type candidateFact struct {
	key          desiredextension.CarrierKey
	loadIdentity hostrelation.HostLoadIdentity
}

type sequenceRowFact struct {
	loadIdentity hostrelation.HostLoadIdentity
	key          desiredextension.CarrierKey
	correlated   bool
}

type sequenceFact struct {
	classID   hostrelation.OrderClassID
	sequence  hostrelation.PhysicalSequenceID
	authority relationobserve.SequenceAuthority
	revision  relationobserve.SequenceRevision
	rows      []sequenceRowFact
}

func validateInput(input Input) error {
	if input.ManifestRoot == "" ||
		!filepath.IsAbs(input.ManifestRoot) ||
		filepath.Clean(input.ManifestRoot) != input.ManifestRoot {
		return fmt.Errorf("extension import manifest root must be absolute and clean")
	}
	if len(input.Targets) == 0 || len(input.Scopes) == 0 {
		return fmt.Errorf("extension import requires targets and scopes")
	}
	targets := make(map[target.Target]struct{}, len(input.Targets))
	for _, selected := range input.Targets {
		if _, err := target.ParseTarget(string(selected)); err != nil {
			return err
		}
		if _, duplicate := targets[selected]; duplicate {
			return fmt.Errorf("extension import target %q is duplicated", selected)
		}
		targets[selected] = struct{}{}
	}
	scopes := make(map[target.Scope]struct{}, len(input.Scopes))
	for _, selected := range input.Scopes {
		if selected != target.ScopeProject && selected != target.ScopeGlobal {
			return fmt.Errorf("extension import scope %q is unsupported", selected)
		}
		if _, duplicate := scopes[selected]; duplicate {
			return fmt.Errorf("extension import scope %q is duplicated", selected)
		}
		scopes[selected] = struct{}{}
	}
	for index, existing := range input.Existing {
		if err := existing.Validate(); err != nil {
			return fmt.Errorf("existing extension[%d]: %w", index, err)
		}
	}
	return nil
}
