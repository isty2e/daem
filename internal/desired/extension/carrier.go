package extension

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/credentialtext"
	"github.com/isty2e/daem/internal/target"
)

// Carrier identifies a host-native extension relation family.
type Carrier string

const (
	CarrierClaudeCodePlugin     Carrier = "claude-code-plugin"
	CarrierCodexPlugin          Carrier = "codex-plugin"
	CarrierOpenCodePlugin       Carrier = "opencode-plugin"
	CarrierPiPackage            Carrier = "pi-package"
	CarrierAntigravityCLIPlugin Carrier = "antigravity-cli-plugin"
)

// ParseCarrier validates the closed current carrier vocabulary.
func ParseCarrier(value string) (Carrier, error) {
	carrier := Carrier(value)
	if _, err := contractFor(carrier); err == nil {
		return carrier, nil
	}
	supported := SupportedCarriers()
	names := make([]string, 0, len(supported))
	for _, candidate := range supported {
		names = append(names, fmt.Sprintf("%q", candidate))
	}
	if len(names) > 1 {
		names[len(names)-1] = "and " + names[len(names)-1]
	}
	return "", fmt.Errorf(
		"unsupported extension carrier %q; supported carriers are %s",
		value,
		strings.Join(names, ", "),
	)
}

// SupportedCarriers returns the closed current carrier vocabulary in stable
// append-only order.
func SupportedCarriers() []Carrier {
	carriers := make([]Carrier, 0, len(carrierContracts))
	for _, contract := range carrierContracts {
		carriers = append(carriers, contract.carrier)
	}
	return carriers
}

// SourceKind identifies the host-native source namespace of a relation.
type SourceKind string

const (
	SourceKindMarketplace SourceKind = "marketplace"
	SourceKindHostSource  SourceKind = "host-source"
)

// SourceRef is one immutable host-native extension source reference.
type SourceRef struct {
	kind SourceKind
	ref  string
}

// MarketplaceSelector is one validated PLUGIN@MARKETPLACE source selector.
type MarketplaceSelector struct {
	plugin      string
	marketplace string
}

// NewSourceRef constructs a validated source reference without resolving it.
func NewSourceRef(kind SourceKind, ref string) (SourceRef, error) {
	if !utf8.ValidString(ref) {
		return SourceRef{}, fmt.Errorf("extension source must be valid UTF-8")
	}
	if strings.TrimSpace(ref) != ref || ref == "" {
		return SourceRef{}, fmt.Errorf("extension source must be non-empty and contain no leading or trailing whitespace")
	}
	if strings.IndexFunc(ref, isUnsafeControl) >= 0 {
		return SourceRef{}, fmt.Errorf("extension source must not contain control characters")
	}
	if strings.HasPrefix(ref, "-") {
		return SourceRef{}, fmt.Errorf("extension source must not begin with '-' because host CLIs may parse it as an option")
	}
	if err := validateCredentialFreeSourceRef(ref); err != nil {
		return SourceRef{}, err
	}
	switch kind {
	case SourceKindMarketplace:
		if _, ok := marketplaceSelector(ref); !ok {
			return SourceRef{}, fmt.Errorf(
				"marketplace source must be PLUGIN@MARKETPLACE and neither component may begin with '-'",
			)
		}
	case SourceKindHostSource:
	default:
		return SourceRef{}, fmt.Errorf("unsupported extension source kind %q", kind)
	}
	return SourceRef{kind: kind, ref: ref}, nil
}

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}

func validateCredentialFreeSourceRef(ref string) error {
	if strings.Contains(ref, "?") {
		return fmt.Errorf("extension source must not contain URL query fields")
	}
	if _, fragment, found := strings.Cut(ref, "#"); found &&
		strings.Contains(fragment, "=") {
		return fmt.Errorf("extension source fragment must not contain assignments")
	}

	parsed, err := url.Parse(ref)
	if err != nil {
		return fmt.Errorf("extension source URL is malformed")
	}
	if strings.Contains(parsed.Fragment, "=") {
		return fmt.Errorf("extension source fragment must not contain assignments")
	}
	if parsed.User != nil {
		_, hasPassword := parsed.User.Password()
		scheme := strings.ToLower(parsed.Scheme)
		if hasPassword ||
			scheme == "http" ||
			scheme == "https" ||
			strings.HasSuffix(scheme, "+http") ||
			strings.HasSuffix(scheme, "+https") {
			return fmt.Errorf("extension source must not contain inline credentials")
		}
	}
	if credentialtext.ContainsCredential(ref) {
		return fmt.Errorf("extension source must not contain credential assignments")
	}
	return nil
}

// Kind returns the source namespace.
func (source SourceRef) Kind() SourceKind { return source.kind }

// Ref returns the exact host-native source reference.
func (source SourceRef) Ref() string { return source.ref }

// MarketplaceSelector returns the structured selector for a valid marketplace source.
func (source SourceRef) MarketplaceSelector() (MarketplaceSelector, bool) {
	if source.kind != SourceKindMarketplace {
		return MarketplaceSelector{}, false
	}
	return marketplaceSelector(source.ref)
}

// String returns the canonical kind:ref source namespace.
func (source SourceRef) String() string {
	if source.kind == "" && source.ref == "" {
		return ""
	}
	return string(source.kind) + ":" + source.ref
}

// Plugin returns the selected plugin name.
func (selector MarketplaceSelector) Plugin() string { return selector.plugin }

// Marketplace returns the selected marketplace name.
func (selector MarketplaceSelector) Marketplace() string { return selector.marketplace }

// String returns the canonical PLUGIN@MARKETPLACE selector.
func (selector MarketplaceSelector) String() string {
	if selector.plugin == "" || selector.marketplace == "" {
		return ""
	}
	return selector.plugin + "@" + selector.marketplace
}

func marketplaceSelector(value string) (MarketplaceSelector, bool) {
	plugin, marketplace, ok := strings.Cut(value, "@")
	if !ok ||
		plugin == "" ||
		marketplace == "" ||
		strings.Contains(marketplace, "@") ||
		strings.HasPrefix(plugin, "-") ||
		strings.HasPrefix(marketplace, "-") {
		return MarketplaceSelector{}, false
	}
	return MarketplaceSelector{plugin: plugin, marketplace: marketplace}, true
}

// ParseSourceRef reconstructs one canonical kind:ref source namespace.
func ParseSourceRef(value string) (SourceRef, error) {
	kind, ref, ok := strings.Cut(value, ":")
	if !ok {
		return SourceRef{}, fmt.Errorf("extension source must use kind:ref form")
	}
	source, err := NewSourceRef(SourceKind(kind), ref)
	if err != nil {
		return SourceRef{}, err
	}
	if source.String() != value {
		return SourceRef{}, fmt.Errorf("extension source %q is not canonical", value)
	}
	return source, nil
}

type carrierContract struct {
	carrier       Carrier
	label         string
	target        target.Target
	allowedScopes map[target.Scope]struct{}
	sourceKind    SourceKind
}

func contractFor(carrier Carrier) (carrierContract, error) {
	for _, contract := range carrierContracts {
		if contract.carrier == carrier {
			return contract, nil
		}
	}
	return carrierContract{}, fmt.Errorf("unsupported extension carrier %q", carrier)
}

// AdmitsTargetScope reports whether this carrier's declaration contract permits
// the target/scope pair. It does not imply that a host realization is implemented.
func (carrier Carrier) AdmitsTargetScope(selectedTarget target.Target, selectedScope target.Scope) bool {
	contract, err := contractFor(carrier)
	if err != nil || contract.target != selectedTarget {
		return false
	}
	_, admitted := contract.allowedScopes[selectedScope]
	return admitted
}

// AdmittedTarget returns the one target owned by this carrier contract. A
// forged carrier has no admitted target.
func (carrier Carrier) AdmittedTarget() (target.Target, bool) {
	contract, err := contractFor(carrier)
	if err != nil {
		return "", false
	}
	return contract.target, true
}

// RequiredSourceKind returns the source namespace owned by this carrier
// contract. A forged carrier has no required source kind.
func (carrier Carrier) RequiredSourceKind() (SourceKind, bool) {
	contract, err := contractFor(carrier)
	if err != nil {
		return "", false
	}
	return contract.sourceKind, true
}

// Label returns the stable human-readable carrier label used in diagnostics. A
// forged carrier has no label.
func (carrier Carrier) Label() (string, bool) {
	contract, err := contractFor(carrier)
	if err != nil {
		return "", false
	}
	return contract.label, true
}

// AdmittedScopes returns the carrier's complete desired scope vocabulary in
// stable order. A forged carrier has no admitted scopes.
func (carrier Carrier) AdmittedScopes() []target.Scope {
	contract, err := contractFor(carrier)
	if err != nil {
		return nil
	}
	scopes := make([]target.Scope, 0, len(contract.allowedScopes))
	for scope := range contract.allowedScopes {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	return scopes
}

func newCarrierContract(
	carrier Carrier,
	label string,
	selected target.Target,
	sourceKind SourceKind,
	scopes ...target.Scope,
) carrierContract {
	allowed := make(map[target.Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		allowed[scope] = struct{}{}
	}
	return carrierContract{
		carrier:       carrier,
		label:         label,
		target:        selected,
		allowedScopes: allowed,
		sourceKind:    sourceKind,
	}
}

var carrierContracts = []carrierContract{
	newCarrierContract(
		CarrierClaudeCodePlugin,
		"Claude Code plugin extension",
		target.TargetClaudeCode,
		SourceKindMarketplace,
		target.ScopeProject,
		target.ScopeGlobal,
	),
	newCarrierContract(
		CarrierCodexPlugin,
		"Codex plugin extension",
		target.TargetCodex,
		SourceKindMarketplace,
		target.ScopeGlobal,
	),
	newCarrierContract(
		CarrierOpenCodePlugin,
		"OpenCode plugin extension",
		target.TargetOpenCode,
		SourceKindHostSource,
		target.ScopeProject,
		target.ScopeGlobal,
	),
	newCarrierContract(
		CarrierPiPackage,
		"Pi package extension",
		target.TargetPi,
		SourceKindHostSource,
		target.ScopeProject,
		target.ScopeGlobal,
	),
	newCarrierContract(
		CarrierAntigravityCLIPlugin,
		"Antigravity CLI plugin extension",
		target.TargetAntigravityCLI,
		SourceKindHostSource,
		target.ScopeGlobal,
	),
}

// CarrierKey identifies one desired host-native carrier independently of
// declaration ID. It is not a lowered SubjectID or current host inventory.
type CarrierKey struct {
	carrier Carrier
	target  target.Target
	scope   target.Scope
	source  SourceRef
}

// NewCarrierKey validates and constructs one canonical desired carrier key.
func NewCarrierKey(
	carrier Carrier,
	selectedTarget target.Target,
	selectedScope target.Scope,
	source SourceRef,
) (CarrierKey, error) {
	validatedCarrier, err := ParseCarrier(string(carrier))
	if err != nil {
		return CarrierKey{}, err
	}
	contract, err := contractFor(validatedCarrier)
	if err != nil {
		return CarrierKey{}, err
	}
	validatedTarget, err := target.ParseTarget(string(selectedTarget))
	if err != nil {
		return CarrierKey{}, err
	}
	if validatedTarget != contract.target {
		return CarrierKey{}, fmt.Errorf(
			"extension carrier %q supports only target %q, got %q",
			validatedCarrier,
			contract.target,
			validatedTarget,
		)
	}
	validatedScope, err := target.ParseScope(string(selectedScope))
	if err != nil {
		return CarrierKey{}, err
	}
	if _, ok := contract.allowedScopes[validatedScope]; !ok {
		return CarrierKey{}, fmt.Errorf("extension carrier %q does not support scope %q", validatedCarrier, validatedScope)
	}
	validatedSource, err := NewSourceRef(source.kind, source.ref)
	if err != nil {
		return CarrierKey{}, err
	}
	if validatedSource.kind != contract.sourceKind {
		return CarrierKey{}, fmt.Errorf(
			"extension carrier %q requires source kind %q, got %q",
			validatedCarrier,
			contract.sourceKind,
			validatedSource.kind,
		)
	}
	if err := validateCarrierSource(validatedCarrier, validatedSource); err != nil {
		return CarrierKey{}, err
	}
	return CarrierKey{
		carrier: validatedCarrier,
		target:  validatedTarget,
		scope:   validatedScope,
		source:  validatedSource,
	}, nil
}

func validateCarrierSource(carrier Carrier, source SourceRef) error {
	if carrier != CarrierCodexPlugin {
		return nil
	}
	selector, ok := source.MarketplaceSelector()
	if !ok {
		return fmt.Errorf("Codex plugin carrier requires source selector PLUGIN@MARKETPLACE")
	}
	for _, segment := range []struct {
		label string
		value string
	}{
		{label: "plugin", value: selector.Plugin()},
		{label: "marketplace", value: selector.Marketplace()},
	} {
		for _, character := range segment.value {
			if character > unicode.MaxASCII ||
				(!unicode.IsLetter(character) &&
					!unicode.IsDigit(character) &&
					character != '_' &&
					character != '-') {
				return fmt.Errorf(
					"Codex plugin carrier %s segment %q must contain only ASCII letters, digits, '_' or '-'",
					segment.label,
					segment.value,
				)
			}
		}
	}
	return nil
}

// Validate rejects a zero or forged carrier key.
func (key CarrierKey) Validate() error {
	expected, err := NewCarrierKey(key.carrier, key.target, key.scope, key.source)
	if err != nil {
		return err
	}
	if key != expected {
		return fmt.Errorf("extension carrier key does not match canonical identity")
	}
	return nil
}

// Carrier returns the host-native carrier family.
func (key CarrierKey) Carrier() Carrier { return key.carrier }

// Target returns the carrier host target.
func (key CarrierKey) Target() target.Target { return key.target }

// Scope returns the carrier host locality.
func (key CarrierKey) Scope() target.Scope { return key.scope }

// Source returns the canonical host-native source reference carried by this key.
func (key CarrierKey) Source() SourceRef { return key.source }
