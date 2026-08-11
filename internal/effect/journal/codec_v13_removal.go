package journal

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func (persisted *recoveryRemovalIntent) UnmarshalJSON(content []byte) error {
	if persisted == nil {
		return fmt.Errorf("recovery removal intent destination is nil")
	}
	if err := requireRecoveryJSONFields(
		content,
		"recovery removal intent",
		"scope",
		"destination",
		"namespace_authority",
		"states",
	); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return fmt.Errorf("recovery removal intent must be a JSON object: %w", err)
	}
	if err := requireRecoveryJSONArrayMaximum(
		fields["states"],
		"recovery removal intent states",
		recovery.MaximumRemovalStatesPerIntent,
	); err != nil {
		return err
	}
	type wire recoveryRemovalIntent
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryRemovalIntent(decoded)
	return nil
}

func requireRecoveryJSONArrayMaximum(
	content []byte,
	document string,
	maximum int,
) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	opening, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s must be a JSON array: %w", document, err)
	}
	if opening != json.Delim('[') {
		return fmt.Errorf("%s must be a JSON array", document)
	}
	count := 0
	for decoder.More() {
		count++
		if count > maximum {
			return fmt.Errorf("%s count exceeds maximum %d", document, maximum)
		}
		var element json.RawMessage
		if err := decoder.Decode(&element); err != nil {
			return fmt.Errorf("%s element[%d]: %w", document, count-1, err)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s closing delimiter: %w", document, err)
	}
	if closing != json.Delim(']') {
		return fmt.Errorf("%s has invalid closing delimiter", document)
	}
	return nil
}

func (persisted *recoveryRemovalNamespaceAuthority) UnmarshalJSON(content []byte) error {
	if persisted == nil {
		return fmt.Errorf("recovery removal namespace destination is nil")
	}
	if err := requireRecoveryJSONFields(
		content,
		"recovery removal namespace authority",
		"variant",
		"residue_name",
		"cleanup_name",
	); err != nil {
		return err
	}
	type wire recoveryRemovalNamespaceAuthority
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryRemovalNamespaceAuthority(decoded)
	return nil
}

func (persisted *recoveryRemovalState) UnmarshalJSON(content []byte) error {
	if persisted == nil {
		return fmt.Errorf("recovery removal state destination is nil")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return fmt.Errorf("recovery removal state must be a JSON object: %w", err)
	}
	if fields == nil {
		return fmt.Errorf("recovery removal state must be a JSON object")
	}
	before, hasBefore := fields["before"]
	expected, hasExpected := fields["expected_after"]
	if hasBefore == hasExpected {
		return fmt.Errorf("recovery removal state must contain exactly one state variant")
	}
	value := before
	name := "before"
	if !hasBefore {
		value = expected
		name = "expected_after"
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return fmt.Errorf("recovery removal state field %q must not be null", name)
	}
	type wire recoveryRemovalState
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryRemovalState(decoded)
	return nil
}
