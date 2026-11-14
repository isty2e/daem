package record

import "fmt"

func (record *native17Root) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("native17 root destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "reference", "kind", "provenance"); err != nil {
		return err
	}
	type wire native17Root
	var value wire
	if err := decodeRecoveryJSONStrict(content, &value); err != nil {
		return err
	}
	*record = native17Root(value)
	return nil
}

func (record *native17Document) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("native17 document destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "captured_target", "writer", "aliases", "extent", "selected_subjects", "original", "expected", "exclusive_claims", "binding_intents", "sequences"); err != nil {
		return err
	}
	type wire native17Document
	var value wire
	if err := decodeRecoveryJSONStrict(content, &value); err != nil {
		return err
	}
	*record = native17Document(value)
	return nil
}

func (record *native17Writer) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("native17 writer destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "root", "coordinate"); err != nil {
		return err
	}
	type wire native17Writer
	var value wire
	if err := decodeRecoveryJSONStrict(content, &value); err != nil {
		return err
	}
	*record = native17Writer(value)
	return nil
}

func (record *native17Alias) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("native17 alias destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "address", "root", "coordinate", "policy", "selected_observation", "observed_before"); err != nil {
		return err
	}
	type wire native17Alias
	var value wire
	if err := decodeRecoveryJSONStrict(content, &value); err != nil {
		return err
	}
	*record = native17Alias(value)
	return nil
}

func (record *nativeSequenceWitness) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("native sequence destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "document", "path", "syntax", "removed", "order"); err != nil {
		return err
	}
	type wire nativeSequenceWitness
	var value wire
	if err := decodeRecoveryJSONStrict(content, &value); err != nil {
		return err
	}
	*record = nativeSequenceWitness(value)
	return nil
}
