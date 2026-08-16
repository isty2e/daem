package clipresent

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"strings"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/credentialtext"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const (
	maximumIdentityDisclosureBytes       = 256
	maximumCarrierDerivedDisclosureBytes = 1024
)

type identityDisclosure struct {
	value    string
	redacted bool
}

func identityDisclosureFor(value string) identityDisclosure {
	if !identityRequiresRedaction(value) {
		return identityDisclosure{value: value}
	}
	return redactedIdentityDisclosure(value)
}

func verboseIdentityDisclosureFor(value string) identityDisclosure {
	if !identityRequiresSensitiveRedaction(value) {
		return identityDisclosure{value: value}
	}
	return redactedIdentityDisclosure(value)
}

func carrierDerivedIdentityDisclosureFor(value string) identityDisclosure {
	if len(value) <= maximumCarrierDerivedDisclosureBytes &&
		!identityRequiresCredentialRedaction(value) {
		return identityDisclosure{value: value}
	}
	return redactedIdentityDisclosure(value)
}

func hostLoadIdentityDisclosureFor(
	selectedTarget target.Target,
	classID hostrelation.OrderClassID,
	value string,
) identityDisclosure {
	disclosure := identityDisclosureFor(value)
	profileTarget, capability, admitted := profile.ExtensionOrderCapabilityForClass(classID)
	if disclosure.Redacted() ||
		(admitted && profileTarget == selectedTarget &&
			extensiontopology.HostLoadIdentityPrivacy(capability.Carrier(), value) ==
				extensiontopology.CarrierSourceIdentityPublic) {
		return disclosure
	}
	return redactedIdentityDisclosure(value)
}

func lockHostLoadIdentityDisclosureFor(
	classID hostrelation.OrderClassID,
	value string,
) identityDisclosure {
	selectedTarget, _, admitted := profile.ExtensionOrderCapabilityForClass(classID)
	if !admitted {
		return redactedIdentityDisclosure(value)
	}
	return hostLoadIdentityDisclosureFor(selectedTarget, classID, value)
}

func (disclosure identityDisclosure) Value() string {
	return disclosure.value
}

func (disclosure identityDisclosure) Redacted() bool {
	return disclosure.redacted
}

func identityRequiresRedaction(value string) bool {
	if identityRequiresSensitiveRedaction(value) {
		return true
	}
	lowerValue := strings.ToLower(value)
	if filepath.IsAbs(value) ||
		strings.HasPrefix(lowerValue, "file:") ||
		strings.Contains(lowerValue, ":file:") ||
		strings.HasPrefix(lowerValue, "local:") ||
		credentialtext.ContainsAssignment(value) ||
		hasLocalPathPrefix(value) ||
		isWindowsAbsolutePath(value) {
		return true
	}
	return false
}

func identityRequiresSensitiveRedaction(value string) bool {
	return len(value) > maximumIdentityDisclosureBytes ||
		identityRequiresCredentialRedaction(value)
}

func identityRequiresCredentialRedaction(value string) bool {
	if strings.Contains(value, "?") ||
		hasURLUserInfo(value) ||
		credentialtext.ContainsCredential(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.Contains(value, "://")
	}
	return parsed.User != nil ||
		parsed.RawQuery != "" ||
		strings.Contains(parsed.Fragment, "=") ||
		strings.Contains(strings.ToLower(value), "%3d")
}

type carrierIdentityDisclosure struct {
	sourceNamespace           identityDisclosure
	sourceRef                 identityDisclosure
	relationSubjectKey        identityDisclosure
	managedInstanceKey        identityDisclosure
	verboseSourceNamespace    identityDisclosure
	verboseSourceRef          identityDisclosure
	verboseRelationSubjectKey identityDisclosure
	verboseManagedInstanceKey identityDisclosure
	carrierSubject            *planJSONSubject
	relationSubject           *planJSONSubject
	verboseCarrierSubject     *planJSONSubject
	verboseRelationSubject    *planJSONSubject
}

type carrierSourceIdentityDisclosure struct {
	sourceNamespace        identityDisclosure
	sourceRef              identityDisclosure
	verboseSourceNamespace identityDisclosure
	verboseSourceRef       identityDisclosure
	carrierSubject         *planJSONSubject
	verboseCarrierSubject  *planJSONSubject
	public                 func(string) identityDisclosure
	verbose                func(string) identityDisclosure
}

func carrierSourceIdentityDisclosureFor(
	carrier extensiontopology.Carrier,
) carrierSourceIdentityDisclosure {
	sourceRef := identityDisclosureFor(carrier.Source().Ref())
	publicSource := carrierSourceAllowsPublicDisclosure(carrier)
	disclose := func(value string) identityDisclosure {
		if publicSource {
			return carrierDerivedIdentityDisclosureFor(value)
		}
		disclosure := identityDisclosureFor(value)
		if !disclosure.Redacted() {
			return redactedIdentityDisclosure(value)
		}
		return disclosure
	}
	if !publicSource && !sourceRef.Redacted() {
		sourceRef = redactedIdentityDisclosure(carrier.Source().Ref())
	}
	verboseSourceRef := verboseIdentityDisclosureFor(carrier.Source().Ref())
	verboseRedactDerivedIdentity := verboseSourceRef.Redacted()
	verboseDisclose := func(value string) identityDisclosure {
		disclosure := verboseIdentityDisclosureFor(value)
		if verboseRedactDerivedIdentity && !disclosure.Redacted() {
			return redactedIdentityDisclosure(value)
		}
		return disclosure
	}

	carrierSubject := planJSONSubjectFor(carrier.SubjectID())
	verboseCarrierSubject := planJSONSubjectFor(carrier.SubjectID())
	if carrierSubject != nil {
		name := disclose(carrierSubject.Name)
		carrierSubject.Name = name.Value()
		carrierSubject.NameRedacted = name.Redacted()
	}
	if verboseCarrierSubject != nil {
		name := verboseDisclose(verboseCarrierSubject.Name)
		verboseCarrierSubject.Name = name.Value()
		verboseCarrierSubject.NameRedacted = name.Redacted()
	}

	return carrierSourceIdentityDisclosure{
		sourceNamespace:        disclose(carrier.Source().String()),
		sourceRef:              sourceRef,
		verboseSourceNamespace: verboseDisclose(carrier.Source().String()),
		verboseSourceRef:       verboseSourceRef,
		carrierSubject:         carrierSubject,
		verboseCarrierSubject:  verboseCarrierSubject,
		public:                 disclose,
		verbose:                verboseDisclose,
	}
}

func desiredExtensionIdentityDisclosureFor(
	extension desiredextension.Extension,
) carrierSourceIdentityDisclosure {
	carrier, err := extensiontopology.NewCarrier(extension.CarrierKey())
	if err == nil {
		return carrierSourceIdentityDisclosureFor(carrier)
	}
	sourceRef := redactedIdentityDisclosure(extension.Source().Ref())
	sourceNamespace := redactedIdentityDisclosure(extension.Source().String())
	verboseSourceRef := verboseIdentityDisclosureFor(extension.Source().Ref())
	verboseSourceNamespace := verboseIdentityDisclosureFor(extension.Source().String())
	return carrierSourceIdentityDisclosure{
		sourceNamespace:        sourceNamespace,
		sourceRef:              sourceRef,
		verboseSourceNamespace: verboseSourceNamespace,
		verboseSourceRef:       verboseSourceRef,
		public:                 redactedIdentityDisclosure,
		verbose:                verboseIdentityDisclosureFor,
	}
}

func carrierIdentityDisclosureFor(
	identity durablecarrier.ManagedCarrierIdentity,
) carrierIdentityDisclosure {
	source := carrierSourceIdentityDisclosureFor(identity.Carrier())
	relationSubject := planJSONSubjectFor(identity.RelationSubject())
	verboseRelationSubject := planJSONSubjectFor(identity.RelationSubject())
	if relationSubject != nil {
		name := source.public(relationSubject.Name)
		relationSubject.Name = name.Value()
		relationSubject.NameRedacted = name.Redacted()
	}
	if verboseRelationSubject != nil {
		name := source.verbose(verboseRelationSubject.Name)
		verboseRelationSubject.Name = name.Value()
		verboseRelationSubject.NameRedacted = name.Redacted()
	}
	return carrierIdentityDisclosure{
		sourceNamespace:           source.sourceNamespace,
		sourceRef:                 source.sourceRef,
		relationSubjectKey:        source.public(string(identity.ExpectedRelation().SubjectKey())),
		managedInstanceKey:        source.public(string(identity.ExpectedRelation().ManagedInstanceKey())),
		verboseSourceNamespace:    source.verboseSourceNamespace,
		verboseSourceRef:          source.verboseSourceRef,
		verboseRelationSubjectKey: source.verbose(string(identity.ExpectedRelation().SubjectKey())),
		verboseManagedInstanceKey: source.verbose(string(identity.ExpectedRelation().ManagedInstanceKey())),
		carrierSubject:            source.carrierSubject,
		relationSubject:           relationSubject,
		verboseCarrierSubject:     source.verboseCarrierSubject,
		verboseRelationSubject:    verboseRelationSubject,
	}
}

func carrierSourceAllowsPublicDisclosure(carrier extensiontopology.Carrier) bool {
	if identityDisclosureFor(carrier.Source().Ref()).Redacted() {
		return false
	}
	interpreted, err := extensiontopology.InterpretCarrierSource(carrier.Key())
	return err == nil &&
		interpreted.IdentityPrivacy() == extensiontopology.CarrierSourceIdentityPublic
}

type lockIdentityProjection struct {
	before map[topology.SubjectID]carrierIdentityDisclosure
	after  map[topology.SubjectID]carrierIdentityDisclosure
}

type lockIdentitySide uint8

const (
	lockIdentityBefore lockIdentitySide = iota
	lockIdentityAfter
)

func newLockIdentityProjection(file lock.File, delta lock.Delta) lockIdentityProjection {
	projection := lockIdentityProjection{
		before: make(map[topology.SubjectID]carrierIdentityDisclosure),
		after:  make(map[topology.SubjectID]carrierIdentityDisclosure),
	}
	for _, contract := range file.Locked.Subjects() {
		projection.add(lockIdentityAfter, contract)
	}
	for _, entry := range delta.Entries() {
		if entry.Status != lock.DeltaStatusAdded {
			projection.add(lockIdentityBefore, entry.Before)
		}
		if entry.Status != lock.DeltaStatusRemoved {
			projection.add(lockIdentityAfter, entry.After)
		}
	}
	return projection
}

func (projection lockIdentityProjection) add(
	side lockIdentitySide,
	contract lock.LockedSubjectContract,
) {
	identities := projection.after
	if side == lockIdentityBefore {
		identities = projection.before
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		if disclosure, ok := unclassifiedCarrierDisclosureFor(contract); ok {
			identities[contract.SubjectID()] = disclosure
		}
		return
	}
	disclosure := carrierIdentityDisclosureFor(identity)
	identities[identity.RelationSubject()] = disclosure
	identities[identity.CarrierSubject()] = disclosure
}

func unclassifiedCarrierDisclosureFor(
	contract lock.LockedSubjectContract,
) (carrierIdentityDisclosure, bool) {
	spec, ok := contract.Realization()
	if !ok || spec.Kind() != realization.RealizationDelegatedRelation {
		return carrierIdentityDisclosure{}, false
	}
	relation, _ := spec.DelegatedRelation()
	subject := planJSONSubjectFor(contract.SubjectID())
	verboseSubject := planJSONSubjectFor(contract.SubjectID())
	if subject != nil {
		name := redactedIdentityDisclosure(subject.Name)
		subject.Name = name.Value()
		subject.NameRedacted = true
	}
	if verboseSubject != nil {
		name := verboseIdentityDisclosureFor(verboseSubject.Name)
		verboseSubject.Name = name.Value()
		verboseSubject.NameRedacted = name.Redacted()
	}
	return carrierIdentityDisclosure{
		sourceNamespace:           redactedIdentityDisclosure(relation.SourceNamespace()),
		relationSubjectKey:        redactedIdentityDisclosure(string(relation.ExpectedRelation().SubjectKey())),
		managedInstanceKey:        redactedIdentityDisclosure(string(relation.ExpectedRelation().ManagedInstanceKey())),
		verboseSourceNamespace:    verboseIdentityDisclosureFor(relation.SourceNamespace()),
		verboseRelationSubjectKey: verboseIdentityDisclosureFor(string(relation.ExpectedRelation().SubjectKey())),
		verboseManagedInstanceKey: verboseIdentityDisclosureFor(string(relation.ExpectedRelation().ManagedInstanceKey())),
		relationSubject:           subject,
		verboseRelationSubject:    verboseSubject,
	}, true
}

func (projection lockIdentityProjection) carrierFor(
	subject topology.SubjectID,
	side lockIdentitySide,
) (carrierIdentityDisclosure, bool) {
	identities := projection.after
	if side == lockIdentityBefore {
		identities = projection.before
	}
	disclosure, ok := identities[subject]
	return disclosure, ok
}

func (projection lockIdentityProjection) subject(
	subject topology.SubjectID,
	side lockIdentitySide,
	verbose bool,
) jsonSubjectID {
	name := identityDisclosureFor(subject.Key())
	if verbose {
		name = verboseIdentityDisclosureFor(subject.Key())
	}
	if disclosure, ok := projection.carrierFor(subject, side); ok {
		projected := disclosure.relationSubject
		if verbose {
			projected = disclosure.verboseRelationSubject
		}
		if subject.Kind() == topology.SubjectCarrier {
			projected = disclosure.carrierSubject
			if verbose {
				projected = disclosure.verboseCarrierSubject
			}
		}
		if projected != nil {
			return jsonSubjectID{
				Kind:         projected.Kind,
				Namespace:    projected.Namespace,
				Name:         projected.Name,
				NameRedacted: projected.NameRedacted,
			}
		}
	}
	return jsonSubjectID{
		Kind:         string(subject.Kind()),
		Namespace:    subject.Namespace(),
		Name:         name.Value(),
		NameRedacted: name.Redacted(),
	}
}

func redactedIdentityDisclosure(value string) identityDisclosure {
	digest := sha256.Sum256([]byte(value))
	return identityDisclosure{
		value:    "redacted:sha256:" + hex.EncodeToString(digest[:]),
		redacted: true,
	}
}

func hasLocalPathPrefix(value string) bool {
	for _, prefix := range []string{
		"./", "../", "~/", `.\`, `..\`, `~\`, `\\`,
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func hasURLUserInfo(value string) bool {
	remaining := value
	for {
		marker := strings.Index(remaining, "://")
		if marker < 0 {
			return false
		}
		authority := remaining[marker+3:]
		if end := strings.IndexAny(authority, "/?#"); end >= 0 {
			authority = authority[:end]
		}
		if strings.Contains(authority, "@") {
			return true
		}
		remaining = remaining[marker+3:]
	}
}

func isWindowsAbsolutePath(value string) bool {
	if len(value) < 3 || value[1] != ':' ||
		(value[2] != '/' && value[2] != '\\') {
		return false
	}
	drive := value[0]
	return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
}
