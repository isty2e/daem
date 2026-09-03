// Package operationplan is the I/O-free algebra for operation authority facts,
// mutation domains, revision roles, exact fingerprint projections, typed
// effect envelopes, and semantic reservation demand.
package operationplan

import (
	"fmt"
	"strconv"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// FactKind is the closed authority-fact taxonomy.
type FactKind uint8

const (
	FactLogical FactKind = iota + 1
	FactPhysical
	FactRoute
	FactRecoveryBarrier
	FactRecoveryRootIdentity
	FactStateDirIdentity
	FactRemovalContinuation
	FactRemovalParentIdentity
)

func (kind FactKind) validate() error {
	switch kind {
	case FactLogical, FactPhysical, FactRoute, FactRecoveryBarrier,
		FactRecoveryRootIdentity, FactStateDirIdentity,
		FactRemovalContinuation, FactRemovalParentIdentity:
		return nil
	default:
		return fmt.Errorf("operationplan: invalid authority fact kind %d", kind)
	}
}

func (kind FactKind) wireName() (string, error) {
	switch kind {
	case FactLogical:
		return "logical", nil
	case FactPhysical:
		return "physical", nil
	case FactRoute:
		return "route", nil
	case FactRecoveryBarrier:
		return "recovery_barrier", nil
	case FactRecoveryRootIdentity:
		return "recovery-root-identity", nil
	case FactStateDirIdentity:
		return "state-dir-identity", nil
	case FactRemovalContinuation:
		return "removal-continuation", nil
	case FactRemovalParentIdentity:
		return "removal-parent-identity", nil
	default:
		return "", fmt.Errorf("operationplan: invalid authority fact kind %d", kind)
	}
}

func (kind FactKind) producesDomain() bool {
	switch kind {
	case FactLogical, FactPhysical, FactRoute, FactRemovalContinuation:
		return true
	default:
		return false
	}
}

// Fact is one closed authority observation. It is not a lease.
type Fact struct {
	kind        FactKind
	path        string
	access      mutation.AccessMode
	effect      mutation.PathEffect
	target      string
	scope       string
	family      string
	containment mutation.RouteContainment
	identity    string
}

// Kind returns the closed fact taxonomy member.
func (fact Fact) Kind() FactKind { return fact.kind }

// Path returns the fact path, which may be empty for route facts.
func (fact Fact) Path() string { return fact.path }

// Access returns the requested mutation access.
func (fact Fact) Access() mutation.AccessMode { return fact.access }

// Effect returns the path-effect class.
func (fact Fact) Effect() mutation.PathEffect { return fact.effect }

func (fact Fact) coverKey() string {
	return strconv.Itoa(int(fact.kind)) + "\x00" +
		fact.path + "\x00" +
		strconv.Itoa(int(fact.access)) + "\x00" +
		strconv.Itoa(int(fact.effect)) + "\x00" +
		fact.target + "\x00" +
		fact.scope + "\x00" +
		fact.family + "\x00" +
		strconv.Itoa(int(fact.containment)) + "\x00" +
		fact.identity
}

func applyRefreshSortKey(fact Fact) (string, error) {
	kind, err := fact.kind.wireName()
	if err != nil {
		return "", err
	}
	return kind + "\x00" + fact.path + "\x00" +
		strconv.Itoa(int(fact.access)) + "\x00" +
		strconv.Itoa(int(fact.effect)) + "\x00" +
		fact.target + "\x00" + fact.scope + "\x00" +
		fact.family + "\x00" +
		strconv.Itoa(int(fact.containment)), nil
}

func recoverSortKey(fact Fact) (string, error) {
	kind, err := fact.kind.wireName()
	if err != nil {
		return "", err
	}
	return kind + "\x00" + fact.path + "\x00" +
		strconv.Itoa(int(fact.access)) + "\x00" +
		strconv.Itoa(int(fact.effect)) + "\x00" +
		fact.target + "\x00" + fact.scope + "\x00" +
		fact.identity, nil
}

// FactsCover reports whether every required fact is present in available.
func FactsCover(available []Fact, required []Fact) bool {
	index := make(map[string]struct{}, len(available))
	for _, fact := range available {
		index[fact.coverKey()] = struct{}{}
	}
	for _, fact := range required {
		if _, present := index[fact.coverKey()]; !present {
			return false
		}
	}
	return true
}
