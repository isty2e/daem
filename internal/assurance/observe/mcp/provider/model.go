// Package provider models current provider evidence for provider-mediated MCP
// projections without owning carrier, route, or managed-config authority.
package provider

import (
	"fmt"
	"sort"
	"strings"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// State classifies the precision and profile compatibility of one current
// provider installation.
type State string

const (
	StateCurrent      State = "current"
	StateAbsent       State = "absent"
	StateUnobservable State = "unobservable"
	StateIncompatible State = "incompatible"
)

// ObservationInput contains one selected contribution and the MCP subjects
// that consume it.
type ObservationInput struct {
	Contribution extensiontopology.ContributionReference
	Carrier      durablecarrier.ManagedCarrierIdentity
	Consumers    []topology.SubjectID
	State        State
	Version      string
	MappedCodec  aggregate.CodecContractID
	Detail       string
}

// Observation is immutable, redaction-safe provider evidence shared by every
// selected MCP projection that consumes one contribution.
type Observation struct {
	contribution extensiontopology.ContributionReference
	carrier      durablecarrier.ManagedCarrierIdentity
	consumers    []topology.SubjectID
	state        State
	version      string
	mappedCodec  aggregate.CodecContractID
	detail       string
}

// NewObservation validates and normalizes one provider observation.
func NewObservation(input ObservationInput) (Observation, error) {
	if err := input.Contribution.Validate(); err != nil {
		return Observation{}, fmt.Errorf("MCP provider contribution: %w", err)
	}
	if err := input.Carrier.Validate(); err != nil {
		return Observation{}, fmt.Errorf("MCP provider carrier: %w", err)
	}
	if input.Contribution.ProviderSubjectID() != input.Carrier.CarrierSubject() {
		return Observation{}, fmt.Errorf("MCP provider contribution does not belong to carrier")
	}
	consumers := append([]topology.SubjectID(nil), input.Consumers...)
	sort.Slice(consumers, func(left int, right int) bool {
		return consumers[left].String() < consumers[right].String()
	})
	if len(consumers) == 0 {
		return Observation{}, fmt.Errorf("MCP provider observation requires at least one consumer")
	}
	for index, consumer := range consumers {
		if err := consumer.Validate(); err != nil {
			return Observation{}, fmt.Errorf("MCP provider consumer[%d]: %w", index, err)
		}
		if index != 0 && consumer == consumers[index-1] {
			return Observation{}, fmt.Errorf("duplicate MCP provider consumer %q", consumer)
		}
	}

	switch input.State {
	case StateCurrent:
		if input.Version == "" || strings.TrimSpace(input.Version) != input.Version {
			return Observation{}, fmt.Errorf("current MCP provider requires an exact version")
		}
		if input.MappedCodec == "" {
			return Observation{}, fmt.Errorf("current MCP provider requires a mapped codec")
		}
		if input.Detail != "" {
			return Observation{}, fmt.Errorf("current MCP provider cannot carry failure detail")
		}
	case StateAbsent:
		if input.Version != "" || input.MappedCodec != "" || input.Detail != "" {
			return Observation{}, fmt.Errorf("absent MCP provider cannot carry version, codec, or detail")
		}
	case StateUnobservable:
		if input.Version != "" || input.MappedCodec != "" || strings.TrimSpace(input.Detail) == "" {
			return Observation{}, fmt.Errorf("unobservable MCP provider requires only failure detail")
		}
	case StateIncompatible:
		if input.Version == "" || input.MappedCodec != "" || strings.TrimSpace(input.Detail) == "" {
			return Observation{}, fmt.Errorf(
				"incompatible MCP provider requires exact version and failure detail",
			)
		}
	default:
		return Observation{}, fmt.Errorf("MCP provider state %q is unsupported", input.State)
	}

	return Observation{
		contribution: input.Contribution,
		carrier:      input.Carrier,
		consumers:    consumers,
		state:        input.State,
		version:      input.Version,
		mappedCodec:  input.MappedCodec,
		detail:       input.Detail,
	}, nil
}

func (observation Observation) Contribution() extensiontopology.ContributionReference {
	return observation.contribution
}

func (observation Observation) Carrier() durablecarrier.ManagedCarrierIdentity {
	return observation.carrier
}

func (observation Observation) Consumers() []topology.SubjectID {
	return append([]topology.SubjectID(nil), observation.consumers...)
}

func (observation Observation) State() State { return observation.state }

func (observation Observation) Version() string { return observation.version }

func (observation Observation) MappedCodec() aggregate.CodecContractID {
	return observation.mappedCodec
}

func (observation Observation) Detail() string { return observation.detail }
