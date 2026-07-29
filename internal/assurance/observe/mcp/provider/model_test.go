package provider

import (
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestObservationOwnsOneProviderAndDefensiveSortedConsumers(t *testing.T) {
	contribution, identity := providerObservationFixture(t, "provider")
	second := mustProviderConsumer(t, "zeta")
	first := mustProviderConsumer(t, "alpha")
	observation, err := NewObservation(ObservationInput{
		Contribution: contribution,
		Carrier:      identity,
		Consumers:    []topology.SubjectID{second, first},
		State:        StateCurrent,
		Version:      "2.15.0",
		MappedCodec:  aggregate.MCPCodecPiAdapterStdio,
	})
	if err != nil {
		t.Fatalf("NewObservation returned error: %v", err)
	}
	consumers := observation.Consumers()
	if len(consumers) != 2 || consumers[0] != first || consumers[1] != second {
		t.Fatalf("Consumers = %#v, want deterministic order", consumers)
	}
	consumers[0] = second
	if observation.Consumers()[0] != first {
		t.Fatal("Consumers returned mutable provider observation state")
	}
}

func TestObservationRejectsDuplicateConsumerAndForeignCarrier(t *testing.T) {
	contribution, identity := providerObservationFixture(t, "provider")
	consumer := mustProviderConsumer(t, "context7")
	_, err := NewObservation(ObservationInput{
		Contribution: contribution,
		Carrier:      identity,
		Consumers:    []topology.SubjectID{consumer, consumer},
		State:        StateAbsent,
	})
	if err == nil {
		t.Fatal("NewObservation accepted duplicate consumers")
	}

	_, foreign := providerObservationFixtureWithSource(
		t,
		"foreign",
		"npm:pi-mcp-adapter@^2.14.0",
	)
	_, err = NewObservation(ObservationInput{
		Contribution: contribution,
		Carrier:      foreign,
		Consumers:    []topology.SubjectID{consumer},
		State:        StateAbsent,
	})
	if err == nil {
		t.Fatal("NewObservation accepted a foreign provider carrier")
	}
}

func TestObservationStatePayloadLaws(t *testing.T) {
	contribution, identity := providerObservationFixture(t, "provider")
	consumer := mustProviderConsumer(t, "context7")
	base := ObservationInput{
		Contribution: contribution,
		Carrier:      identity,
		Consumers:    []topology.SubjectID{consumer},
	}
	for _, test := range []struct {
		name   string
		mutate func(*ObservationInput)
	}{
		{
			name: "current without version",
			mutate: func(input *ObservationInput) {
				input.State = StateCurrent
				input.MappedCodec = aggregate.MCPCodecPiAdapterStdio
			},
		},
		{
			name: "absent with version",
			mutate: func(input *ObservationInput) {
				input.State = StateAbsent
				input.Version = "2.15.0"
			},
		},
		{
			name: "unobservable without detail",
			mutate: func(input *ObservationInput) {
				input.State = StateUnobservable
			},
		},
		{
			name: "incompatible with mapped codec",
			mutate: func(input *ObservationInput) {
				input.State = StateIncompatible
				input.Version = "3.0.0"
				input.MappedCodec = aggregate.MCPCodecPiAdapterStdio
				input.Detail = "outside profile"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := NewObservation(input); err == nil {
				t.Fatal("NewObservation accepted invalid state payload")
			}
		})
	}
}

func providerObservationFixture(
	t *testing.T,
	name string,
) (extensiontopology.ContributionReference, durablecarrier.ManagedCarrierIdentity) {
	t.Helper()
	return providerObservationFixtureWithSource(
		t,
		name,
		"npm:pi-mcp-adapter@^2.13.0",
	)
}

func providerObservationFixtureWithSource(
	t *testing.T,
	name string,
	sourceRef string,
) (extensiontopology.ContributionReference, durablecarrier.ManagedCarrierIdentity) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		sourceRef,
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := extensiontopology.NewContribution(
		carrier,
		extensiontopology.ContributionSpec{Kind: "mcp-client", Key: "default"},
	)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := desiredextension.New(desiredextension.Spec{
		Name: name, Carrier: desiredextension.CarrierPiPackage,
		Target: target.TargetPi, Scope: target.ScopeProject, Source: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	relationSubject, err := extensiontopology.Relation(declared)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(source.Ref())
	if err != nil {
		t.Fatal(err)
	}
	relation, err := hostrelation.Derive(key, relationSubject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(
		carrier,
		relationSubject,
		relation,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contribution.Reference(), identity
}

func mustProviderConsumer(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"pi.project.mcp-server",
		name,
	)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}
