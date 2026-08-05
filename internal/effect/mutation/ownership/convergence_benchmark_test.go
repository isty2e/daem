package ownership

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func BenchmarkClaimPreparationConvergence(b *testing.B) {
	for _, size := range []int{1, 10, 100} {
		transitions := benchmarkAcquireTransitions(size)
		set, err := NewClaimTransitionSet(transitions)
		if err != nil {
			b.Fatal(err)
		}
		convergence, err := set.Preparation()
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("batch/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, _, err := convergence.Apply(outputownership.EmptyRegistry()); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("sequential-reference/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				registry := outputownership.EmptyRegistry()
				for _, transition := range transitions {
					registry, err = registry.Apply(
						transition.Address(),
						transition.Before(),
						transition.Prepared(),
					)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func benchmarkAcquireTransitions(count int) []ClaimTransition {
	root := filepath.Join(string(filepath.Separator), "benchmark")
	statefile := pathtest.Exact(filepath.Join(root, "state.json"))
	authority, err := stateauthority.New(statefile, filepath.Join(root, "daem.toml"))
	if err != nil {
		panic(err)
	}
	transitions := make([]ClaimTransition, 0, count)
	for index := range count {
		address, err := outputownership.NewManagedAddress(
			pathtest.Exact(filepath.Join(root, fmt.Sprintf("output-%06d", index))),
			"",
		)
		if err != nil {
			panic(err)
		}
		transition, err := NewAcquireTransition(address, authority, "benchmark-operation")
		if err != nil {
			panic(err)
		}
		transitions = append(transitions, transition)
	}
	return transitions
}
