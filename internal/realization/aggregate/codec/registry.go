// Package aggregatecodec composes the finite set of concrete host aggregate
// codecs admitted by the product.
package aggregatecodec

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
)

var catalog = mustCatalog()

// Catalog returns the immutable product aggregate codec composition.
func Catalog() aggregate.CodecCatalog {
	return catalog
}

func mustCatalog() aggregate.CodecCatalog {
	catalog, err := buildCatalog()
	if err != nil {
		panic(err)
	}
	return catalog
}

func buildCatalog() (aggregate.CodecCatalog, error) {
	owners := make(map[aggregate.CodecContractID]string)
	codecs := make([]aggregate.Codec, 0)
	for _, placement := range aggregate.ImplementedHookPlacements() {
		contractID := placement.CodecContractID()
		codec, ok := hookcodec.For(contractID)
		if err := validateCodecRegistration(owners, "Hook", contractID, codec, ok); err != nil {
			return aggregate.CodecCatalog{}, err
		}
		codecs = append(codecs, codec)
	}
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		contractID := placement.CodecContractID()
		codec, ok := mcpcodec.For(contractID)
		if err := validateCodecRegistration(owners, "MCP", contractID, codec, ok); err != nil {
			return aggregate.CodecCatalog{}, err
		}
		codecs = append(codecs, codec)
	}
	return aggregate.NewCodecCatalog(codecs)
}

func validateCodecRegistration(
	owners map[aggregate.CodecContractID]string,
	owner string,
	contractID aggregate.CodecContractID,
	codec aggregate.Codec,
	present bool,
) error {
	if !present || codec == nil {
		return fmt.Errorf("%s aggregate codec %q is missing", owner, contractID)
	}
	if codec.ContractID() != contractID {
		return fmt.Errorf(
			"%s aggregate codec %q reports contract %q",
			owner,
			contractID,
			codec.ContractID(),
		)
	}
	if previous, duplicate := owners[contractID]; duplicate {
		return fmt.Errorf(
			"aggregate codec contract %q is owned by both %s and %s",
			contractID,
			previous,
			owner,
		)
	}
	owners[contractID] = owner
	return nil
}
