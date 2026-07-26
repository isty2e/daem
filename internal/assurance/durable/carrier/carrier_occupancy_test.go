package carrier_test

import (
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierOccupancyIndexesOnlyExactDaemKnownConsumers(t *testing.T) {
	first := carrierFixtureFor(t, "alpha", "shared@official", target.ScopeGlobal)
	second := carrierFixtureFor(t, "beta", "shared@official", target.ScopeGlobal)
	foreign := carrierFixtureFor(t, "foreign", "other@official", target.ScopeGlobal)

	firstClaim := claimForFixture(t, first, mustStateAuthority(t, t.TempDir(), "alpha.toml"))
	secondClaim := claimForFixture(t, second, mustStateAuthority(t, t.TempDir(), "beta.toml"))
	foreignClaim := claimForFixture(t, foreign, mustStateAuthority(t, t.TempDir(), "foreign.toml"))

	occupancy, err := durablecarrier.NewCarrierOccupancy(
		first.carrier,
		[]durablecarrier.ManagedCarrierClaim{secondClaim, foreignClaim, firstClaim},
	)
	if err != nil {
		t.Fatalf("NewCarrierOccupancy returned error: %v", err)
	}
	consumers := occupancy.DaemKnownConsumers()
	if len(consumers) != 2 || occupancy.DaemKnownConsumerCount() != 2 || occupancy.IsDaemKnownEmpty() {
		t.Fatalf("daem-known consumers = %#v", consumers)
	}
	firstConsumer := consumers[0]
	if occupancy.IsOnlyDaemKnownConsumer(firstConsumer) {
		t.Fatal("multi-consumer occupancy reported a sole daem-known consumer")
	}
	consumers[0] = durablecarrier.CarrierConsumer{}
	if occupancy.DaemKnownConsumers()[0].ManagedInstanceKey() == "" {
		t.Fatal("consumer result was not defensively copied")
	}

	single, err := durablecarrier.NewCarrierOccupancy(first.carrier, []durablecarrier.ManagedCarrierClaim{firstClaim})
	if err != nil {
		t.Fatalf("single NewCarrierOccupancy returned error: %v", err)
	}
	only := single.DaemKnownConsumers()[0]
	if !single.IsOnlyDaemKnownConsumer(only) {
		t.Fatal("single exact claim was not the only daem-known consumer")
	}
}

func TestCarrierOccupancyRejectsDuplicateConsumerClaims(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeGlobal)
	claim := claimForFixture(t, fixture, mustStateAuthority(t, t.TempDir(), "daem.toml"))
	if _, err := durablecarrier.NewCarrierOccupancy(
		fixture.carrier,
		[]durablecarrier.ManagedCarrierClaim{claim, claim},
	); err == nil || !strings.Contains(err.Error(), "duplicate daem-known consumer") {
		t.Fatalf("error = %v, want duplicate consumer rejection", err)
	}
}

func TestCarrierOccupancyZeroValueIsInvalid(t *testing.T) {
	if err := (durablecarrier.CarrierOccupancy{}).Validate(); err == nil {
		t.Fatal("zero CarrierOccupancy validated")
	}
}
