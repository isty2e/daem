package store

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

type documentBindingRecord struct {
	Destination        string              `json:"destination"`
	PathAuthority      pathAuthorityRecord `json:"path_authority"`
	StatefileAuthority pathAuthorityRecord `json:"statefile_authority"`
	ManifestPath       string              `json:"manifest_path"`
}

func decodeDocumentBindings(records []documentBindingRecord) ([]ownership.DocumentBinding, error) {
	bindings := make([]ownership.DocumentBinding, 0, len(records))
	for index, record := range records {
		destination, err := output.Parse(record.Destination)
		if err != nil {
			return nil, fmt.Errorf("document_bindings[%d] destination: %w", index, err)
		}
		physical, err := pathauthority.NewExact(record.PathAuthority.Key, record.PathAuthority.Witness)
		if err != nil {
			return nil, fmt.Errorf("document_bindings[%d] physical namespace: %w", index, err)
		}
		statefile, err := pathauthority.NewExact(record.StatefileAuthority.Key, record.StatefileAuthority.Witness)
		if err != nil {
			return nil, fmt.Errorf("document_bindings[%d] statefile authority: %w", index, err)
		}
		owner, err := stateauthority.New(statefile, record.ManifestPath)
		if err != nil {
			return nil, fmt.Errorf("document_bindings[%d] owner: %w", index, err)
		}
		binding, err := ownership.NewDocumentBinding(owner, destination, physical)
		if err != nil {
			return nil, fmt.Errorf("document_bindings[%d]: %w", index, err)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func persistedDocumentBindings(bindings []ownership.DocumentBinding) []documentBindingRecord {
	records := make([]documentBindingRecord, 0, len(bindings))
	for _, binding := range bindings {
		physical := binding.PathAuthority()
		statefile := binding.Owner().StatefileAuthority()
		records = append(records, documentBindingRecord{
			Destination: binding.Destination().String(),
			PathAuthority: pathAuthorityRecord{
				Key: physical.Key(), Witness: physical.Witness(),
			},
			StatefileAuthority: pathAuthorityRecord{
				Key: statefile.Key(), Witness: statefile.Witness(),
			},
			ManifestPath: binding.Owner().ManifestPath(),
		})
	}
	return records
}
