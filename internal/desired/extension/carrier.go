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

// maxAuthoredSourceRefBytes bounds one newly authored extension source
// before any structural or credential validation, so a single authored or
// imported source cannot amplify validation, lock, or host-argument work.
// The bound applies at authoring ingress, not in the canonical constructor,
// so sources persisted under earlier schemas keep decoding.
const maxAuthoredSourceRefBytes = 2048

// NewAuthoredSourceRef validates a source supplied by authoring ingress:
// manifests, normalization, authoring commands, and adoption, where
// untrusted text first enters the system. It validates the raw spelling and
// bounded canonical form once at ingress. Durable or derived reconstruction
// uses NewSourceRef, which carries no authored policy.
func NewAuthoredSourceRef(kind SourceKind, ref string) (SourceRef, error) {
	if len(ref) > maxAuthoredSourceRefBytes {
		return SourceRef{}, fmt.Errorf("extension source exceeds the admission length limit")
	}
	if strings.Contains(ref, "%") {
		// A source whose escapes do not fully resolve within the bounded
		// fixed point is uninspectable; authoring rejects it instead of
		// admitting text later inspection cannot classify.
		if _, _, stable := credentialtext.CanonicalDecode(ref); !stable {
			return SourceRef{}, fmt.Errorf("extension source contains malformed percent-encoding")
		}
	}
	source, err := NewSourceRef(kind, ref)
	if err != nil {
		return SourceRef{}, err
	}
	if err := validateCredentialFreeSourceRef(kind, ref); err != nil {
		return SourceRef{}, err
	}
	if !source.CredentialFree() {
		return SourceRef{}, fmt.Errorf("extension source must not contain inline credentials")
	}
	if !source.ControlFree() {
		return SourceRef{}, fmt.Errorf("extension source must not contain control characters")
	}
	return source, nil
}

// NewSourceRef reconstructs one structurally valid source reference without
// granting execution or disclosure authority. Authoring uses NewAuthoredSourceRef.
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

func validateCredentialFreeSourceRef(kind SourceKind, ref string) error {
	// Percent-encoded variants can conceal credential delimiters from the
	// raw-text checks, so the bounded fixed-point decoded form is validated
	// too. NewAuthoredSourceRef already rejected an unstable decoded form.
	decoded := ref
	if strings.Contains(ref, "%") {
		decoded, _, _ = credentialtext.CanonicalDecode(ref)
	}
	if err := validateCredentialFreeSourceRefText(kind, ref); err != nil {
		return err
	}
	if decoded != ref {
		return validateCredentialFreeSourceRefText(kind, decoded)
	}
	return nil
}

func validateCredentialFreeSourceRefText(kind SourceKind, ref string) error {
	if strings.Contains(ref, "?") {
		return fmt.Errorf("extension source must not contain URL query fields")
	}
	if _, fragment, found := strings.Cut(ref, "#"); found &&
		strings.Contains(fragment, "=") {
		return fmt.Errorf("extension source fragment must not contain assignments")
	}
	if kind == SourceKindMarketplace {
		if credentialtext.ContainsCredentialAssignment(ref) {
			return fmt.Errorf("extension source must not contain credential assignments")
		}
		return nil
	}

	// scp-style sources carry no URL query or fragment structure, so URL
	// userinfo parsing applies to every other spelling. A parse failure only
	// means there is no URL structure to inspect here: malformed authority
	// shapes fail closed in the nested URL validator, and raw escape forms
	// fail closed in the stability gate.
	if !scpLikeGitSource(ref) {
		if parsed, err := url.Parse(ref); err == nil {
			if strings.Contains(parsed.Fragment, "=") {
				return fmt.Errorf("extension source fragment must not contain assignments")
			}
			if urlUserInfoCarriesCredential(parsed) {
				return fmt.Errorf("extension source must not contain inline credentials")
			}
		}
	}
	if err := validateNestedURLCredentials(ref); err != nil {
		return err
	}
	if sourceRefContainsCredentialField(kind, ref) {
		return fmt.Errorf("extension source must not contain credential assignments")
	}
	return nil
}

func sourceRefContainsCredentialField(kind SourceKind, ref string) bool {
	if kind == SourceKindMarketplace {
		return credentialtext.ContainsCredentialAssignment(ref)
	}
	return hostSourceContainsCredentialField(ref)
}

// hostSourceContainsCredentialField gives source punctuation to its owning
// grammar before generic key/value inspection. Authority users, hosts, ports,
// and source-kind prefixes are structural; only locator payloads and refs can
// carry credential fields. Userinfo authority is a separate tri-state query.
func hostSourceContainsCredentialField(ref string) bool {
	if strings.HasPrefix(ref, "npm:") {
		spec := strings.TrimPrefix(ref, "npm:")
		if parsed, ok := ParseNPMPackageSpec(spec); ok {
			return parsed.containsCredentialField()
		}
		return credentialtext.ContainsCredential(spec)
	}
	payload := ref
	if strings.HasPrefix(payload, "git:") {
		payload = strings.TrimPrefix(payload, "git:")
	} else if strings.HasPrefix(payload, "github:") {
		payload = strings.TrimPrefix(payload, "github:")
	}
	if _, _, path, ok := splitScpLikeGitSource(payload); ok {
		return credentialtext.ContainsCredential(path)
	}
	if parsed, ok := parseHostSourceURL(payload); ok {
		return credentialtext.ContainsCredential(strings.TrimPrefix(parsed.EscapedPath(), "/")) ||
			credentialtext.ContainsCredential(parsed.Fragment)
	}
	return credentialtext.ContainsCredential(payload)
}

func parseHostSourceURL(ref string) (*url.URL, bool) {
	if !strings.Contains(ref, "://") {
		return nil, false
	}
	parsed, err := url.Parse(ref)
	return parsed, err == nil && parsed.Hostname() != ""
}

// urlUserInfoCarriesCredential reports whether one parsed URL's userinfo is
// credential-bearing: any password, or any userinfo on an http(s) transport.
func urlUserInfoCarriesCredential(parsed *url.URL) bool {
	if parsed.User == nil {
		return false
	}
	_, hasPassword := parsed.User.Password()
	scheme := strings.ToLower(parsed.Scheme)
	return hasPassword ||
		scheme == "http" ||
		scheme == "https" ||
		strings.HasSuffix(scheme, "+http") ||
		strings.HasSuffix(scheme, "+https")
}

// validateNestedURLCredentials interprets each URL embedded in value in one
// linear pass. Scheme-prefixed sources such as "git:https://user:secret@
// host/..." are opaque to one top-level URL parse, so each protocol
// occurrence is classified on its own bounded segment: credential-bearing
// userinfo is rejected, and an authority-shaped segment that cannot be
// parsed is rejected because malformed authorities only obscure userinfo
// inspection.
func validateNestedURLCredentials(value string) error {
	marker := strings.Index(value, "://")
	for marker >= 0 {
		schemeStart := marker
		for schemeStart > 0 && isURLSchemeByte(value[schemeStart-1]) {
			schemeStart--
		}
		segmentEnd := len(value)
		if next := strings.Index(value[marker+3:], "://"); next >= 0 {
			segmentEnd = marker + 3 + next
		}
		segment := value[schemeStart:segmentEnd]
		parsed, err := url.Parse(segment)
		switch {
		case err != nil && authorityHasUserInfoShape(segment):
			return fmt.Errorf("extension source contains a malformed URL authority")
		case err == nil && urlUserInfoCarriesCredential(parsed):
			return fmt.Errorf("extension source must not contain inline credentials")
		}
		if segmentEnd == len(value) {
			return nil
		}
		marker = segmentEnd
	}
	return nil
}

// authorityHasUserInfoShape reports whether segment's URL authority is
// shaped to carry userinfo or a bracketed host, so a parse failure can hide
// credential-relevant structure.
func authorityHasUserInfoShape(segment string) bool {
	authority := segment
	if marker := strings.Index(authority, "://"); marker >= 0 {
		authority = authority[marker+3:]
	}
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	return strings.Contains(authority, "@") || strings.Contains(authority, "[")
}

func isURLSchemeByte(value byte) bool {
	switch {
	case value >= 'A' && value <= 'Z':
		return true
	case value >= 'a' && value <= 'z':
		return true
	case value >= '0' && value <= '9':
		return true
	default:
		return value == '+' || value == '-' || value == '.'
	}
}

// Kind returns the source namespace.
func (source SourceRef) Kind() SourceKind { return source.kind }

// Ref returns the exact host-native source reference.
func (source SourceRef) Ref() string { return source.ref }

// CredentialFree reports whether the selected grammar proves that the raw and
// canonical source carry no query fields, assignment fragments, credential
// fields, or password userinfo. Older durable values may remain structurally
// valid without this execution and disclosure authority. Marketplace ':' and
// '@' bytes are selector data rather than Git or URL userinfo.
func (source SourceRef) CredentialFree() bool {
	decoded, _, stable := credentialtext.CanonicalDecode(source.ref)
	if !stable ||
		validateCredentialFreeSourceRef(source.kind, source.ref) != nil ||
		!credentialFreeSourceRefText(source.kind, source.ref) {
		return false
	}
	if source.kind == SourceKindMarketplace && decoded != source.ref {
		_, ok := marketplaceSelector(decoded)
		return ok && !credentialtext.ContainsCredentialAssignment(decoded)
	}
	return true
}

// ControlFree reports whether raw and canonical source text contain no Unicode
// control or Bidi_Control characters. Authoring rejects such sources; durable
// reconstruction may still carry encoded controls without this effect or
// disclosure authority.
func (source SourceRef) ControlFree() bool {
	decoded, _, stable := credentialtext.CanonicalDecode(source.ref)
	if !stable {
		return false
	}
	return strings.IndexFunc(source.ref, isUnsafeControl) < 0 &&
		strings.IndexFunc(decoded, isUnsafeControl) < 0
}

func credentialFreeSourceRefText(kind SourceKind, ref string) bool {
	switch kind {
	case SourceKindMarketplace:
		_, ok := marketplaceSelector(ref)
		return ok && !credentialtext.ContainsCredentialAssignment(ref)
	case SourceKindHostSource:
		if spec, found := strings.CutPrefix(ref, "npm:"); found {
			if parsed, ok := ParseNPMPackageSpec(spec); ok {
				return parsed.CredentialFree()
			}
		}
		if gitSource, ok := ParseGitSource(ref); ok {
			return gitSource.CredentialFree() &&
				!hostSourceContainsCredentialField(ref)
		}
		return !hostSourceContainsCredentialField(ref) &&
			credentialtext.InspectPasswordUserInfo(ref) ==
				credentialtext.UserInfoAbsent
	default:
		return false
	}
}

// MarketplaceSelector returns the structured selector only when raw and
// canonical source forms carry credential-free marketplace authority.
func (source SourceRef) MarketplaceSelector() (MarketplaceSelector, bool) {
	if source.kind != SourceKindMarketplace || !source.CredentialFree() {
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
		return SourceRef{}, fmt.Errorf("extension source namespace is not canonical")
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
