package carrierclaim

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
)

const locatedRegistryVersion = 3

type registryImageRecord struct {
	Version        int                         `json:"version"`
	Claims         []claimDTO                  `json:"claims"`
	PathReferences []pathreferencecodec.Record `json:"path_references"`
}

// MarshalImage encodes version3 without changing the default carrier Store.
func MarshalImage(image RegistryImage) ([]byte, error) {
	if err := image.Validate(); err != nil {
		return nil, err
	}
	claims := image.Registry().Claims()
	record := registryImageRecord{Version: locatedRegistryVersion, Claims: make([]claimDTO, 0, len(claims))}
	for _, claim := range claims {
		record.Claims = append(record.Claims, persistedClaim(claim))
	}
	references, err := pathreferencecodec.EncodeTable(image.references)
	if err != nil {
		return nil, err
	}
	record.PathReferences = references
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	content = append(content, '\n')
	if len(content) > maximumRegistryBytes {
		return nil, fmt.Errorf("carrier claim registry exceeds %d bytes", maximumRegistryBytes)
	}
	return content, nil
}

// UnmarshalImage admits version3 only; owner keys never supply missing locators.
func UnmarshalImage(content []byte) (RegistryImage, error) {
	if len(content) > maximumRegistryBytes {
		return RegistryImage{}, fmt.Errorf("carrier claim registry exceeds %d bytes", maximumRegistryBytes)
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "carrier claim registry image", maximumRegistryJSONDepth)
	if err != nil {
		return RegistryImage{}, err
	}
	if version != locatedRegistryVersion {
		return RegistryImage{}, fmt.Errorf("carrier claim registry image requires version %d, got %d", locatedRegistryVersion, version)
	}
	var record registryImageRecord
	if err := decodeRegistryJSON(content, &record); err != nil {
		return RegistryImage{}, err
	}
	registry, err := canonicalClaims(record.Claims)
	if err != nil {
		return RegistryImage{}, err
	}
	references, err := pathreferencecodec.DecodeTable(record.PathReferences)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("carrier claim registry path_references: %w", err)
	}
	return NewRegistryImage(registry, references)
}
