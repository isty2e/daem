package hookcodec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
)

func canonicalJSON(value any) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func decodeHookHostConfig(content []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, fmt.Errorf("hook config JSON is empty")
	}

	var settings map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&settings); err != nil {
		return nil, fmt.Errorf("decode hook config JSON: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("hook config JSON contains multiple values")
	} else if err != io.EOF {
		return nil, err
	}
	if settings == nil {
		return nil, fmt.Errorf("hook config JSON must be an object")
	}

	return settings, nil
}

type canonicalHookContribution struct {
	Event string    `json:"event"`
	Group hookGroup `json:"group"`
}

type hookGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookHandler `json:"hooks"`
}

type hookHandler struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	Condition     string `json:"if,omitempty"`
	Timeout       int    `json:"timeout,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

// CanonicalHookContribution renders one validated subject contribution.
func CanonicalHookContribution(input commandhook.ContributionInput) (string, error) {
	contribution := canonicalHookContribution{
		Event: input.Event,
		Group: hookGroup{
			Matcher: input.Matcher,
			Hooks: []hookHandler{{
				Type: input.Type, Command: input.Command, Condition: input.Condition,
				Timeout: input.Timeout, StatusMessage: input.StatusMessage,
			}},
		},
	}
	if err := contribution.validate(); err != nil {
		return "", err
	}
	content, err := canonicalJSON(contribution)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ValidateCanonicalHookContribution rejects non-canonical or malformed locked Hook bytes.
func ValidateCanonicalHookContribution(content string) error {
	_, err := decodeCanonicalHookContribution(content)
	return err
}

func (contribution canonicalHookContribution) validate() error {
	if strings.TrimSpace(contribution.Event) == "" || strings.TrimSpace(contribution.Event) != contribution.Event {
		return fmt.Errorf("Hook contribution event is required and must be trimmed")
	}
	if strings.TrimSpace(contribution.Group.Matcher) != contribution.Group.Matcher {
		return fmt.Errorf("Hook contribution matcher must be trimmed")
	}
	if len(contribution.Group.Hooks) != 1 {
		return fmt.Errorf("Hook contribution requires exactly one handler")
	}
	handler := contribution.Group.Hooks[0]
	if handler.Type != "command" {
		return fmt.Errorf("Hook contribution handler type %q is unsupported", handler.Type)
	}
	if strings.TrimSpace(handler.Command) == "" || strings.TrimSpace(handler.Command) != handler.Command {
		return fmt.Errorf("Hook contribution command is required and must be trimmed")
	}
	if strings.TrimSpace(handler.Condition) != handler.Condition ||
		strings.TrimSpace(handler.StatusMessage) != handler.StatusMessage {
		return fmt.Errorf("Hook contribution optional text must be trimmed")
	}
	if handler.Timeout < 0 {
		return fmt.Errorf("Hook contribution timeout must not be negative")
	}
	return nil
}

type hookJSONCodec struct {
	contractID aggregate.CodecContractID
}

// For returns the Hook codec implementing contractID.
func For(contractID aggregate.CodecContractID) (aggregate.Codec, bool) {
	if _, ok := aggregate.HookPlacementForCodec(contractID); !ok {
		return nil, false
	}
	return hookJSONCodec{contractID: contractID}, true
}

func (codec hookJSONCodec) ContractID() aggregate.CodecContractID { return codec.contractID }

func (codec hookJSONCodec) ValidateContribution(contribution aggregate.ManagedContribution) error {
	if err := contribution.Validate(); err != nil {
		return err
	}
	placement, ok := aggregate.HookPlacementForCodec(codec.contractID)
	if !ok {
		return fmt.Errorf("aggregate codec contract %q has no Hook placement", codec.contractID)
	}
	expected, err := placement.Contribution(contribution.CanonicalContribution())
	if err != nil {
		return err
	}
	if !expected.Contract().Equal(contribution.Contract()) {
		return fmt.Errorf("Hook contribution does not match its codec placement contract")
	}
	return ValidateCanonicalHookContribution(contribution.CanonicalContribution())
}

func (codec hookJSONCodec) Read(document aggregate.Document, selection aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	contract, failure := codec.selectedHookContract(document, selection)
	if failure != nil {
		return aggregate.Snapshot{}, failure
	}
	if !document.Exists() {
		state, err := aggregate.NewProjectionState(contract, false, false, "")
		if err != nil {
			return aggregate.Snapshot{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
		}
		snapshot, err := aggregate.NewSnapshot(false, selection, []aggregate.ProjectionState{state})
		if err != nil {
			return aggregate.Snapshot{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
		}
		return snapshot, nil
	}

	settings, reason := decodeHookDocument(document.Content())
	if reason != "" {
		return aggregate.Snapshot{}, hookCodecFailure(reason)
	}
	hooks, present := settings["hooks"]
	canonical := ""
	if present {
		canonicalValue, err := canonicalHookProjection(hooks)
		if err != nil {
			return aggregate.Snapshot{}, hookCodecFailure(aggregate.CodecFailureSelectedShapeUnsupported)
		}
		canonical = canonicalValue
	}
	state, err := aggregate.NewProjectionState(contract, true, present, canonical)
	if err != nil {
		return aggregate.Snapshot{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	snapshot, err := aggregate.NewSnapshot(true, selection, []aggregate.ProjectionState{state})
	if err != nil {
		return aggregate.Snapshot{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	return snapshot, nil
}

func (codec hookJSONCodec) Render(document aggregate.Document, plan aggregate.Plan) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	selection, err := plan.Before().Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	if _, failure := codec.selectedHookContract(document, selection); failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	settings := make(map[string]json.RawMessage)
	if document.Exists() {
		var reason aggregate.CodecFailureReason
		settings, reason = decodeHookDocument(document.Content())
		if reason != "" {
			return aggregate.RenderedDocument{}, hookCodecFailure(reason)
		}
	}

	intent := plan.Intents()[0]
	desired, desiredPresent := intent.Desired()
	canonicalProjection := ""
	if desiredPresent {
		canonicalProjection, err = foldHookContributions(desired)
		if err != nil {
			return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
		}
		settings["hooks"] = json.RawMessage(canonicalProjection)
	} else {
		delete(settings, "hooks")
	}

	candidate, failure := renderHookSettings(settings, false)
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	state, err := aggregate.NewProjectionState(intent.Before().Contract(), candidate.Exists(), desiredPresent, canonicalProjection)
	if err != nil {
		return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	expected, err := aggregate.NewSnapshot(candidate.Exists(), selection, []aggregate.ProjectionState{state})
	if err != nil {
		return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	rendered, err := aggregate.NewRenderedDocument(candidate, plan, expected)
	if err != nil {
		return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	return rendered, nil
}

func (codec hookJSONCodec) Restore(document aggregate.Document, baseline aggregate.Snapshot) (aggregate.RenderedDocument, *aggregate.CodecFailure) {
	selection, err := baseline.Selection()
	if err != nil {
		return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	if _, failure := codec.selectedHookContract(document, selection); failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	settings := make(map[string]json.RawMessage)
	if document.Exists() {
		var reason aggregate.CodecFailureReason
		settings, reason = decodeHookDocument(document.Content())
		if reason != "" {
			return aggregate.RenderedDocument{}, hookCodecFailure(reason)
		}
	}
	state := baseline.States()[0]
	if state.Present() {
		if err := validateHookProjection([]byte(state.CanonicalProjection())); err != nil {
			return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
		}
		settings["hooks"] = json.RawMessage(state.CanonicalProjection())
	} else {
		delete(settings, "hooks")
	}
	candidate, failure := renderHookSettings(settings, baseline.DocumentExisted())
	if failure != nil {
		return aggregate.RenderedDocument{}, failure
	}
	restored, err := aggregate.NewRestoredDocument(candidate, baseline)
	if err != nil {
		return aggregate.RenderedDocument{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	return restored, nil
}

func (codec hookJSONCodec) selectedHookContract(document aggregate.Document, selection aggregate.Selection) (aggregate.ProjectionContract, *aggregate.CodecFailure) {
	if err := document.Validate(); err != nil || selection.CodecContractID() != codec.contractID {
		return aggregate.ProjectionContract{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	contracts := selection.Contracts()
	if len(contracts) != 1 {
		return aggregate.ProjectionContract{}, hookCodecFailure(aggregate.CodecFailureSelectedShapeUnsupported)
	}
	placement, ok := aggregate.HookPlacementForCodec(codec.contractID)
	if !ok {
		return aggregate.ProjectionContract{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	expected, err := placement.Contribution("contract")
	if err != nil || !contracts[0].Equal(expected.Contract()) {
		return aggregate.ProjectionContract{}, hookCodecFailure(aggregate.CodecFailurePreservationUndefined)
	}
	return contracts[0], nil
}

func foldHookContributions(set aggregate.ContributionSet) (string, error) {
	hooks := make(map[string][]hookGroup)
	for _, item := range set.Contributions() {
		contribution, err := decodeCanonicalHookContribution(item.Contribution().CanonicalContribution())
		if err != nil {
			return "", err
		}
		hooks[contribution.Event] = append(hooks[contribution.Event], contribution.Group)
	}
	content, err := canonicalJSON(hooks)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func decodeCanonicalHookContribution(content string) (canonicalHookContribution, error) {
	if err := validateNoDuplicateJSONKeys([]byte(content)); err != nil {
		return canonicalHookContribution{}, err
	}
	var contribution canonicalHookContribution
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contribution); err != nil {
		return canonicalHookContribution{}, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return canonicalHookContribution{}, err
	}
	if err := contribution.validate(); err != nil {
		return canonicalHookContribution{}, err
	}
	canonical, err := canonicalJSON(contribution)
	if err != nil || string(canonical) != content {
		return canonicalHookContribution{}, fmt.Errorf("Hook contribution is not canonical")
	}
	return contribution, nil
}

func decodeHookDocument(content []byte) (map[string]json.RawMessage, aggregate.CodecFailureReason) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, aggregate.CodecFailureDocumentMalformed
	}
	if err := validateNoDuplicateJSONKeys(content); err != nil {
		if _, duplicate := err.(duplicateJSONKeyError); duplicate {
			return nil, aggregate.CodecFailureDuplicateKey
		}
		return nil, aggregate.CodecFailureDocumentMalformed
	}
	settings, err := decodeHookHostConfig(content)
	if err != nil {
		return nil, aggregate.CodecFailureDocumentMalformed
	}
	return settings, ""
}

func validateHookProjection(content []byte) error {
	_, err := canonicalHookProjection(content)
	return err
}

func canonicalHookProjection(content []byte) (string, error) {
	var hooks map[string][]hookGroup
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&hooks); err != nil {
		return "", err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return "", err
	}
	if hooks == nil {
		return "", fmt.Errorf("Hook projection must be an object")
	}
	for event, groups := range hooks {
		if strings.TrimSpace(event) == "" || strings.TrimSpace(event) != event {
			return "", fmt.Errorf("Hook projection event is invalid")
		}
		for _, group := range groups {
			if strings.TrimSpace(group.Matcher) != group.Matcher || len(group.Hooks) == 0 {
				return "", fmt.Errorf("Hook projection group is invalid")
			}
			for _, handler := range group.Hooks {
				candidate := canonicalHookContribution{Event: event, Group: hookGroup{Matcher: group.Matcher, Hooks: []hookHandler{handler}}}
				if err := candidate.validate(); err != nil {
					return "", err
				}
			}
		}
	}
	canonical, err := canonicalJSON(hooks)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func renderHookSettings(settings map[string]json.RawMessage, preserveExistingEmpty bool) (aggregate.Document, *aggregate.CodecFailure) {
	if len(settings) == 0 && !preserveExistingEmpty {
		return aggregate.AbsentDocument(), nil
	}
	content, err := canonicalJSON(settings)
	if err != nil {
		return aggregate.Document{}, hookCodecFailure(aggregate.CodecFailureCanonicalInvalid)
	}
	return aggregate.ExistingDocument(content), nil
}

type duplicateJSONKeyError struct{}

func (duplicateJSONKeyError) Error() string { return "JSON object contains a duplicate key" }

func validateNoDuplicateJSONKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return duplicateJSONKeyError{}
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	want := json.Delim('}')
	if delimiter == '[' {
		want = ']'
	}
	if closing != want {
		return fmt.Errorf("unexpected JSON closing delimiter")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("JSON contains multiple values")
}

func hookCodecFailure(reason aggregate.CodecFailureReason) *aggregate.CodecFailure {
	failure, err := aggregate.NewCodecFailure(reason, aggregate.ContentPath(aggregate.HooksContentPath))
	if err != nil {
		panic(err)
	}
	return failure
}
