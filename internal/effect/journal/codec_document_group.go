package journal

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type recoveryDocumentGroup struct {
	CapturedTarget   recoveryDocumentObservation     `json:"captured_target"`
	Writer           recoveryDocumentWriter          `json:"writer"`
	Aliases          []recoveryDocumentAlias         `json:"aliases"`
	Extent           string                          `json:"extent"`
	SelectedSubjects []persistedSubjectRef           `json:"selected_subjects"`
	PiOrder          *recoveryDocumentPiOrder        `json:"pi_order,omitempty"`
	Original         recoveryDocumentImage           `json:"original"`
	Expected         recoveryDocumentImage           `json:"expected"`
	ExclusiveClaims  recoveryDocumentClaimSet        `json:"exclusive_claims"`
	BindingIntents   []recoveryDocumentBindingIntent `json:"binding_intents"`
	Removal          *recoveryDocumentRemoval        `json:"removal,omitempty"`
}

type recoveryDocumentObservation struct {
	Kind               string            `json:"kind"`
	Authority          *pathAuthorityDTO `json:"authority,omitempty"`
	NamespaceAuthority *pathAuthorityDTO `json:"namespace_authority,omitempty"`
}

type recoveryDocumentWriter struct {
	Root         recoveryRootProvenance `json:"root_provenance"`
	RelativePath string                 `json:"relative_path"`
}

type recoveryDocumentLogicalRoot struct {
	Role       string                 `json:"role"`
	Provenance recoveryRootProvenance `json:"provenance"`
}

type recoveryDocumentAlias struct {
	Address             recoveryDocumentAliasRef    `json:"address"`
	LogicalRoot         recoveryDocumentLogicalRoot `json:"logical_root"`
	SelectedObservation recoveryDocumentObservation `json:"selected_observation"`
	ObservedBefore      recoveryDocumentObservation `json:"observed_before"`
}

type recoveryDocumentPiOrder struct {
	Alias       recoveryDocumentAliasRef    `json:"alias"`
	CommandRoot recoveryDocumentLogicalRoot `json:"command_root"`
	Evidence    recoveryPiOrderEvidence     `json:"evidence"`
}

func (record *recoveryDocumentGroup) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("document group destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "captured_target", "writer", "aliases", "extent", "selected_subjects", "original", "expected", "exclusive_claims", "binding_intents"); err != nil {
		return err
	}
	type wire recoveryDocumentGroup
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryDocumentGroup(decoded)
	return nil
}

func (record recoveryDocumentGroup) validate(manifest recoveryRootProvenance) error {
	if err := record.CapturedTarget.validate(); err != nil {
		return err
	}
	if _, err := record.Writer.provenance(); err != nil {
		return err
	}
	relative, err := rootedpath.NewRelativeDestination(record.Writer.RelativePath)
	if err != nil || relative.Path() != record.Writer.RelativePath {
		return fmt.Errorf("document writer requires a canonical relative referent")
	}
	if len(record.Aliases) == 0 || record.SelectedSubjects == nil || record.BindingIntents == nil {
		return fmt.Errorf("document aliases and explicit subject/binding arrays are required")
	}
	for index, alias := range record.Aliases {
		address, err := alias.Address.canonical()
		if err != nil {
			return err
		}
		if err := alias.LogicalRoot.validate(address.Scope(), manifest); err != nil {
			return err
		}
		if err := alias.SelectedObservation.validate(); err != nil {
			return err
		}
		if err := alias.ObservedBefore.validate(); err != nil {
			return err
		}
		if !alias.ObservedBefore.equal(record.CapturedTarget) {
			return fmt.Errorf("document alias before observation differs from its captured target")
		}
		if index != 0 && compareDocumentAliasRefs(record.Aliases[index-1].Address, alias.Address) >= 0 {
			return fmt.Errorf("document aliases must be unique and canonically ordered")
		}
	}
	seen := make(map[topology.SubjectID]struct{}, len(record.SelectedSubjects))
	for _, ref := range record.SelectedSubjects {
		subject, err := ref.canonical()
		if err != nil {
			return err
		}
		if _, duplicate := seen[subject]; duplicate {
			return fmt.Errorf("document selected subjects repeat a full identity")
		}
		seen[subject] = struct{}{}
	}
	switch record.Extent {
	case "whole":
		if len(record.SelectedSubjects) != 0 || record.PiOrder != nil {
			return fmt.Errorf("whole document extent cannot share selected writers")
		}
	case "selected":
	default:
		return fmt.Errorf("unsupported document comparison extent")
	}
	if err := record.Original.validate(); err != nil {
		return err
	}
	if err := record.Expected.validate(); err != nil {
		return err
	}
	if err := record.ExclusiveClaims.validateShape(); err != nil {
		return err
	}
	for _, binding := range record.BindingIntents {
		if _, err := binding.canonical(); err != nil {
			return err
		}
	}
	if record.PiOrder != nil {
		order := record.PiOrder
		address, err := order.Alias.canonical()
		if err != nil {
			return err
		}
		if address.Target() != target.TargetPi || string(address.Scope()) != order.Evidence.Scope {
			return fmt.Errorf("Pi order scope/target differs from its alias")
		}
		settings, err := piOrderDocumentAlias(address.Scope())
		if err != nil || address != settings {
			return fmt.Errorf("Pi order alias does not identify the admitted settings document")
		}
		if !slices.ContainsFunc(record.Aliases, func(alias recoveryDocumentAlias) bool { return alias.Address == order.Alias }) {
			return fmt.Errorf("Pi order has no declared settings alias")
		}
		if err := order.CommandRoot.validate(target.ScopeProject, manifest); err != nil {
			return fmt.Errorf("Pi command root: %w", err)
		}
	}
	return record.validateRemoval()
}

func piOrderDocumentAlias(scope target.Scope) (aggregate.DocumentAddress, error) {
	return pipackage.SettingsDocumentAddress(scope)
}

func (record recoveryDocumentAliasRef) canonical() (aggregate.DocumentAddress, error) {
	selected, err := target.ParseTarget(record.Target)
	if err != nil {
		return aggregate.DocumentAddress{}, err
	}
	scope, err := target.ParseScope(record.Scope)
	if err != nil {
		return aggregate.DocumentAddress{}, err
	}
	destination, err := output.Parse(record.Destination)
	if err != nil {
		return aggregate.DocumentAddress{}, err
	}
	return aggregate.NewDocumentAddress(selected, scope, destination)
}

func compareDocumentAliasRefs(left, right recoveryDocumentAliasRef) int {
	for _, comparison := range []int{cmp.Compare(left.Target, right.Target), cmp.Compare(left.Scope, right.Scope), cmp.Compare(left.Destination, right.Destination)} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func (record recoveryDocumentLogicalRoot) validate(scope target.Scope, manifest recoveryRootProvenance) error {
	if _, err := rootedpath.NewAuthorityProvenance(record.Provenance.PhysicalRoot, record.Provenance.ObjectFingerprint, record.Provenance.MountFingerprint); err != nil {
		return err
	}
	if scope == target.ScopeProject {
		if record.Role != "manifest-root" || record.Provenance != manifest {
			return fmt.Errorf("project document root differs from the captured manifest root")
		}
	} else if scope != target.ScopeGlobal || record.Role != "global-root" {
		return fmt.Errorf("document logical root role differs from its scope")
	}
	return nil
}

func (record recoveryDocumentWriter) provenance() (rootedpath.AuthorityProvenance, error) {
	return rootedpath.NewAuthorityProvenance(record.Root.PhysicalRoot, record.Root.ObjectFingerprint, record.Root.MountFingerprint)
}

func (record recoveryDocumentObservation) validate() error {
	switch record.Kind {
	case "exact":
		if record.Authority == nil || record.NamespaceAuthority != nil {
			return fmt.Errorf("exact observation requires only exact authority")
		}
		_, err := canonicalPathAuthority(*record.Authority)
		return err
	case "provisional":
		_, err := record.provisional()
		return err
	default:
		return fmt.Errorf("unsupported document path observation")
	}
}

func (record recoveryDocumentObservation) provisional() (pathauthority.Provisional, error) {
	if record.Kind != "provisional" || record.Authority == nil || record.NamespaceAuthority == nil {
		return pathauthority.Provisional{}, fmt.Errorf("provisional observation requires candidate and namespace authorities")
	}
	return pathauthority.NewProvisional(record.Authority.Key, record.Authority.Witness, record.NamespaceAuthority.Key, record.NamespaceAuthority.Witness)
}

func (record recoveryDocumentObservation) equal(other recoveryDocumentObservation) bool {
	if record.Kind != other.Kind || record.validate() != nil || other.validate() != nil || *record.Authority != *other.Authority {
		return false
	}
	return record.NamespaceAuthority == nil && other.NamespaceAuthority == nil || record.NamespaceAuthority != nil && other.NamespaceAuthority != nil && *record.NamespaceAuthority == *other.NamespaceAuthority
}

func (record recoveryDocumentObservation) matchesRetainedExact(exact pathauthority.Exact) bool {
	if record.validate() != nil || exact.Validate() != nil {
		return false
	}
	if record.Kind == "exact" {
		return *record.Authority == *persistedPathAuthority(exact)
	}
	missing, err := record.provisional()
	return err == nil && missing.MatchesMissingExact(exact)
}
