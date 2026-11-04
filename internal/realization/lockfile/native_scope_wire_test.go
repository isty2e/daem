package lockfile

import (
	"bytes"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRealizationWireRetainsNativeKeyIdentity(t *testing.T) {
	paths := [][]string{{"ordinary"}, {""}, {"a/b"}, {"a", "b"}, {"~1"}, {"."}, {".."}, {"한글", "0"}, {"line\n\x00key"}, make([]string, 300)}
	seen := make(map[string]bool)
	for _, segments := range paths {
		key, err := output.NewKeyPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		spec := nativeWireRealization(t, key)
		dto := realizationToDTO(spec, true)
		if dto == nil || dto.ManagedAggregate == nil || seen[dto.ManagedAggregate.KeyPath] {
			t.Fatal("native realization lost or collapsed its wire scope")
		}
		seen[dto.ManagedAggregate.KeyPath] = true
		var wire bytes.Buffer
		if err := toml.NewEncoder(&wire).Encode(dto); err != nil {
			t.Fatal(err)
		}
		var decodedDTO realizationDTO
		metadata, err := toml.Decode(wire.String(), &decodedDTO)
		if err != nil || len(metadata.Undecoded()) != 0 {
			t.Fatalf("native realization TOML did not round trip: %v", err)
		}
		decoded, err := realizationFromDTO(&decodedDTO)
		if err != nil || !decoded.Equal(spec) {
			t.Fatalf("native realization wire changed canonical data: %q, %v", key, err)
		}
	}
}

func TestRealizationWireRefusesRootAndMalformedNativeScope(t *testing.T) {
	key, err := output.NewKeyPath("ordinary")
	if err != nil {
		t.Fatal(err)
	}
	spec := nativeWireRealization(t, key)
	for _, invalid := range []string{"", "ordinary", "/bad~", "/bad~2", string([]byte{'/', 0xff})} {
		dto := realizationToDTO(spec, true)
		dto.ManagedAggregate.KeyPath = invalid
		decoded, err := realizationFromDTO(dto)
		if err == nil || decoded != nil {
			t.Fatalf("invalid wire scope returned a partial realization: %q, %v", invalid, err)
		}
	}
}

func nativeWireRealization(t testing.TB, key output.KeyPath) realization.RealizationSpec {
	t.Helper()
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/settings.json"), footprint, "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, "opaque-wire-payload")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := realization.NewManagedAggregateContribution(contribution)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
