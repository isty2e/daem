package profile

import (
	"fmt"
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
)

func TestPiMCPContextCompatibility(t *testing.T) {
	native, err := PiMCPContractForBackend(desiredmcp.BackendNative)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := PiMCPContractForBackend(desiredmcp.BackendAdapter)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		contracts []PiMCPContract
		accepted  bool
	}{
		{"no bindings", nil, true},
		{"native", []PiMCPContract{native}, true},
		{"native peers", []PiMCPContract{native, native}, true},
		{"adapter", []PiMCPContract{adapter}, true},
		{"adapter peers", []PiMCPContract{adapter, adapter}, true},
		{"native then adapter", []PiMCPContract{native, adapter}, false},
		{"adapter then native", []PiMCPContract{adapter, native}, false},
		{"unadmitted contract", []PiMCPContract{{}}, false},
	} {
		for _, declared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/provider=%t", test.name, declared), func(t *testing.T) {
				accepted := test.accepted
				if declared {
					for _, contract := range test.contracts {
						accepted = accepted && contract.Backend() != desiredmcp.BackendNative
					}
				}
				if err := AdmitPiMCPContext(test.contracts, declared); (err == nil) != accepted {
					t.Fatalf("context admission = %v, accepted=%t", err, accepted)
				}
			})
		}
	}
}
