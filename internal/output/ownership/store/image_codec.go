package store

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
)

const locatedRegistryVersion = 5

type registryImageRecord struct {
	Version            int                         `json:"version"`
	Claims             []claimRecord               `json:"claims"`
	DocumentBindings   []documentBindingRecord     `json:"document_bindings"`
	PendingProtections json.RawMessage             `json:"pending_protections,omitempty"`
	PathReferences     []pathreferencecodec.Record `json:"path_references"`
}

// MarshalImage encodes a complete version5 image, without publishing it or
// changing the default Store format.
func MarshalImage(image RegistryImage) ([]byte, error) {
	if err := image.Validate(); err != nil {
		return nil, err
	}
	facts, err := persistedRegistryFacts(image.Registry())
	if err != nil {
		return nil, err
	}
	references, err := pathreferencecodec.EncodeTable(image.references)
	if err != nil {
		return nil, err
	}
	record := registryImageRecord{
		Version: locatedRegistryVersion, Claims: facts.Claims, DocumentBindings: facts.DocumentBindings,
		PendingProtections: facts.PendingProtections, PathReferences: references,
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	content = append(content, '\n')
	if int64(len(content)) > maximumOwnershipRegistryBytes {
		return nil, fmt.Errorf("ownership registry exceeds %d bytes", maximumOwnershipRegistryBytes)
	}
	return content, nil
}

// UnmarshalImage admits version5 only. It never invents locators for older input.
func UnmarshalImage(content []byte) (RegistryImage, error) {
	if int64(len(content)) > maximumOwnershipRegistryBytes {
		return RegistryImage{}, fmt.Errorf("ownership registry exceeds %d bytes", maximumOwnershipRegistryBytes)
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "ownership registry image", maximumOwnershipRegistryJSONDepth)
	if err != nil {
		return RegistryImage{}, err
	}
	if version != locatedRegistryVersion {
		return RegistryImage{}, fmt.Errorf("ownership registry image requires version %d, got %d", locatedRegistryVersion, version)
	}
	var record registryImageRecord
	if err := decodeRegistryJSON(content, &record); err != nil {
		return RegistryImage{}, err
	}
	registry, err := decodeRegistryFacts(registryFactsRecord{
		Claims: record.Claims, DocumentBindings: record.DocumentBindings, PendingProtections: record.PendingProtections,
	})
	if err != nil {
		return RegistryImage{}, err
	}
	references, err := pathreferencecodec.DecodeTable(record.PathReferences)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("ownership registry path_references: %w", err)
	}
	return NewRegistryImage(registry, references)
}
