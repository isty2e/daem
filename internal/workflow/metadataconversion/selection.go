package metadataconversion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
)

type selection struct {
	manifestPath  string
	statefilePath string
	dataDir       string
	barrierDirs   []string
	references    pathauthority.ReferenceTable
}

type selectionRecord struct {
	Version          int                         `json:"version"`
	Kind             string                      `json:"kind"`
	ManifestPath     string                      `json:"manifest_path"`
	StatefilePath    string                      `json:"statefile_path"`
	DataDir          string                      `json:"data_dir"`
	BarrierStateDirs []string                    `json:"barrier_state_dirs"`
	PathReferences   []pathreferencecodec.Record `json:"path_references"`
}

func (value selection) encode() ([]byte, error) {
	if err := value.validate(); err != nil {
		return nil, err
	}
	record := selectionRecord{
		Version: 1, Kind: "metadata-conversion", ManifestPath: value.manifestPath,
		StatefilePath: value.statefilePath, DataDir: value.dataDir,
		BarrierStateDirs: value.barrierDirs,
		PathReferences:   make([]pathreferencecodec.Record, 0),
	}
	for _, reference := range value.references.References() {
		encoded, err := pathreferencecodec.Encode(reference)
		if err != nil {
			return nil, err
		}
		record.PathReferences = append(record.PathReferences, encoded)
	}
	return json.Marshal(record)
}

func decodeSelection(content []byte) (selection, error) {
	if err := jsonstrict.ValidateRecord(content, "metadata conversion selection", 64); err != nil {
		return selection{}, err
	}
	var record selectionRecord
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return selection{}, err
	}
	if record.Version != 1 || record.Kind != "metadata-conversion" || record.PathReferences == nil || record.BarrierStateDirs == nil {
		return selection{}, fmt.Errorf("unsupported or incomplete metadata conversion selection")
	}
	identities := make([]pathauthority.Exact, 0, len(record.PathReferences))
	references := make([]pathauthority.Reference, 0, len(record.PathReferences))
	seen := make(map[pathauthority.Exact]bool, len(record.PathReferences))
	for _, encoded := range record.PathReferences {
		reference, err := pathreferencecodec.Decode(encoded)
		if err != nil {
			return selection{}, err
		}
		if seen[reference.Identity()] {
			return selection{}, fmt.Errorf("metadata selection repeats a reference identity")
		}
		seen[reference.Identity()] = true
		identities = append(identities, reference.Identity())
		references = append(references, reference)
	}
	table, err := pathauthority.NewReferenceTable(identities, references)
	if err != nil {
		return selection{}, err
	}
	value := selection{
		manifestPath: record.ManifestPath, statefilePath: record.StatefilePath,
		dataDir: record.DataDir, barrierDirs: record.BarrierStateDirs, references: table,
	}
	return value, value.validate()
}

func (value selection) validate() error {
	for _, path := range []string{value.manifestPath, value.statefilePath, value.dataDir} {
		if _, err := metadataPathDepth(path); err != nil {
			return err
		}
	}
	if len(value.barrierDirs) == 0 {
		return fmt.Errorf("metadata selection requires participant local barriers")
	}
	seen := make(map[string]bool, len(value.barrierDirs))
	for _, path := range value.barrierDirs {
		if _, err := metadataPathDepth(path); err != nil {
			return err
		}
		if seen[path] {
			return fmt.Errorf("metadata selection repeats a local barrier")
		}
		seen[path] = true
	}
	if filepath.Base(value.statefilePath) == string(filepath.Separator) {
		return fmt.Errorf("metadata selection requires a State namespace")
	}
	return value.references.Validate()
}
