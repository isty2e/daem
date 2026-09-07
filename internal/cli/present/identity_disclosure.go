package clipresent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/credentialtext"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
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

func carrierSourceRefDisclosureFor(source desiredextension.SourceRef) identityDisclosure {
	value := source.Ref()
	if len(value) > maximumIdentityDisclosureBytes ||
		!source.CredentialFree() ||
		!source.ControlFree() {
		return redactedIdentityDisclosure(value)
	}
	_, _, stable := credentialtext.CanonicalDecode(value)
	if !stable {
		return redactedIdentityDisclosure(value)
	}
	return identityDisclosure{value: value}
}

// grammarProvenCarrierDerivedIdentityDisclosureFor projects identities whose
// dynamic source component has already passed SourceRef's contextual grammar.
// Re-parsing the derived string as a generic URL would give marketplace ':'
// and '@' characters a meaning they do not have in that namespace.
func grammarProvenCarrierDerivedIdentityDisclosureFor(
	value string,
	sourceRef string,
) identityDisclosure {
	if len(value) > maximumCarrierDerivedDisclosureBytes {
		return redactedIdentityDisclosure(value)
	}
	_, _, stable := credentialtext.CanonicalDecode(value)
	if !stable || sourceRef == "" {
		return redactedIdentityDisclosure(value)
	}
	inspection := strings.ReplaceAll(value, sourceRef, "carrier-source")
	quotedSource, _ := json.Marshal(sourceRef)
	escapedSource := string(quotedSource[1 : len(quotedSource)-1])
	if escapedSource != sourceRef {
		inspection = strings.ReplaceAll(inspection, escapedSource, "carrier-source")
	}
	if identityRequiresCredentialRedaction(inspection) {
		return redactedIdentityDisclosure(value)
	}
	return identityDisclosure{value: value}
}

func hostLoadIdentityDisclosureFor(
	selectedTarget target.Target,
	classID hostrelation.OrderClassID,
	value string,
) identityDisclosure {
	disclosure := identityDisclosureFor(value)
	profileTarget, capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(classID)
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
	selectedTarget, _, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(classID)
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
	decoded, _, stable := credentialtext.CanonicalDecode(value)
	if !stable {
		return true
	}
	return localPathIdentityShape(value) ||
		decoded != value && localPathIdentityShape(decoded)
}

// localPathIdentityShape reports whether value, or any ref-delimited part of
// it, is shaped like a machine-local path. '@' and '#' delimit refs in git
// spellings, and each part is classified on its own so a local path carried
// by a ref cannot hide inside an otherwise public locator.
func localPathIdentityShape(value string) bool {
	if credentialtext.ContainsAssignment(value) || localPathShape(value) {
		return true
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '@' || r == '#' }) {
		if localPathShape(part) {
			return true
		}
	}
	return false
}

func localPathShape(value string) bool {
	lowerValue := strings.ToLower(value)
	return filepath.IsAbs(value) ||
		strings.HasPrefix(lowerValue, "file:") ||
		strings.Contains(lowerValue, ":file:") ||
		strings.HasPrefix(lowerValue, "local:") ||
		hasLocalPathPrefix(value) ||
		isWindowsAbsolutePath(value) ||
		containsFileScheme(lowerValue)
}

// containsFileScheme reports whether lowerValue contains a file-scheme
// occurrence at a scheme boundary followed by '/'. File URLs locate
// machine-local state wherever they appear in an identity.
func containsFileScheme(lowerValue string) bool {
	searchFrom := 0
	for {
		index := strings.Index(lowerValue[searchFrom:], "file:")
		if index < 0 {
			return false
		}
		index += searchFrom
		searchFrom = index + 1
		end := index + len("file:")
		if end >= len(lowerValue) || lowerValue[end] != '/' {
			continue
		}
		if index == 0 || !isSchemeByte(lowerValue[index-1]) {
			return true
		}
	}
}

func isSchemeByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= '0' && value <= '9' ||
		value == '+' || value == '-' || value == '.'
}

func identityRequiresSensitiveRedaction(value string) bool {
	if len(value) > maximumIdentityDisclosureBytes {
		return true
	}
	// Unresolved escapes leave credential inspection blind, so every
	// disclosure path, default and verbose alike, fails closed here instead
	// of trusting a best-effort scan.
	decoded, _, stable := credentialtext.CanonicalDecode(value)
	if !stable {
		return true
	}
	if strings.IndexFunc(value, identityHasUnsafeControl) >= 0 ||
		strings.IndexFunc(decoded, identityHasUnsafeControl) >= 0 {
		return true
	}
	return identityRequiresCredentialRedaction(value)
}

func identityHasUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}

func identityRequiresCredentialRedaction(value string) bool {
	passwordUserInfo := credentialtext.InspectPasswordUserInfo(value)
	if strings.Contains(value, "?") ||
		credentialtext.ContainsURLUserInfo(value) ||
		passwordUserInfo == credentialtext.UserInfoUninspectable ||
		credentialtext.ContainsCredential(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.Contains(value, "://")
	}
	return parsed.User != nil ||
		parsed.RawQuery != "" ||
		strings.Contains(parsed.Fragment, "=")
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
	grammarSourceRef := carrierSourceRefDisclosureFor(carrier.Source())
	publicSource := carrierSourceAllowsPublicDisclosure(carrier)
	disclose := func(value string) identityDisclosure {
		if publicSource && !grammarSourceRef.Redacted() {
			return grammarProvenCarrierDerivedIdentityDisclosureFor(
				value,
				carrier.Source().Ref(),
			)
		}
		return redactedIdentityDisclosure(value)
	}
	sourceRef := grammarSourceRef
	if !publicSource && !sourceRef.Redacted() {
		sourceRef = redactedIdentityDisclosure(sourceRef.Value())
	}
	verboseSourceRef := grammarSourceRef
	verboseRedactDerivedIdentity := verboseSourceRef.Redacted()
	verboseDisclose := func(value string) identityDisclosure {
		if verboseRedactDerivedIdentity {
			return redactedIdentityDisclosure(value)
		}
		return grammarProvenCarrierDerivedIdentityDisclosureFor(
			value,
			carrier.Source().Ref(),
		)
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
	if carrierSourceRefDisclosureFor(carrier.Source()).Redacted() {
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

func isWindowsAbsolutePath(value string) bool {
	if len(value) < 3 || value[1] != ':' ||
		(value[2] != '/' && value[2] != '\\') {
		return false
	}
	drive := value[0]
	return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
}
