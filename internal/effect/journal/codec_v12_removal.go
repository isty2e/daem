package journal

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	type wire recoveryRemovalIntent
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*persisted = recoveryRemovalIntent(decoded)
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
