package ownership

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

func TestDocumentClaimChangeReshapesCompleteFragments(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	path := filepath.Join(root, "settings.json")
	parent := documentTestClaim(t, path, owner, "a")
	left := documentTestClaim(t, path, owner, "a", "b")
	right := documentTestClaim(t, path, owner, "a", "c")
	untouched := documentTestClaim(t, path, owner, "a!")
	binding := testDocumentBinding(t, root, "foreign", "@pi-agent/settings.json", path)
	parentValue, _ := PresentClaim(parent)
	leftValue, _ := PresentClaim(left)
	if _, err := NewClaimConvergence([]ClaimChange{
		mustClaimChange(t, left.Address(), leftValue, NoClaim()),
		mustClaimChange(t, parent.Address(), NoClaim(), parentValue),
	}); err == nil {
		t.Fatal("independent row protocol unexpectedly admits an overlapping shape transition")
	}

	for _, test := range []struct {
		name           string
		before, target []Claim
	}{
		{name: "expand", before: []Claim{left, right}, target: []Claim{parent}},
		{name: "contract", before: []Claim{parent}, target: []Claim{left, right}},
	} {
		t.Run(test.name, func(t *testing.T) {
			change := mustDocumentChange(t, test.before, test.target)
			cover := change.FootprintCover()
			if len(cover) != 1 || !cover[0].Equal(parent.Address()) {
				t.Fatalf("shape cover = %v, want the supplied parent only", cover)
			}
			input := []DocumentClaimChange{change}
			convergence := mustDocumentConvergence(t, nil, input...)
			input[0] = DocumentClaimChange{}
			initial, err := NewRegistry(append(slices.Clone(test.before), untouched), binding)
			if err != nil {
				t.Fatal(err)
			}
			want, err := NewRegistry(append(slices.Clone(test.target), untouched), binding)
			if err != nil {
				t.Fatal(err)
			}

			next, changed, err := convergence.Apply(initial)
			if err != nil || !changed || !next.Equal(want) {
				t.Fatalf("shape convergence lost exact claims or unrelated binding: %v", err)
			}
			again, changed, err := convergence.Apply(next)
			if err != nil || changed || !again.Equal(want) {
				t.Fatalf("shape retry is not idempotent: %v", err)
			}
			if !claimsEqual(initial.Claims(), mustDocumentChange(t, append(slices.Clone(test.before), untouched), nil).Expected()) {
				t.Fatal("pure convergence changed its input")
			}
		})
	}
}

func TestDocumentClaimChangeRefusesIncompleteMixedOrForeignFragments(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign.json"), filepath.Join(root, "foreign.toml"))
	path := filepath.Join(root, "settings.json")
	left := documentTestClaim(t, path, owner, "a", "b")
	right := documentTestClaim(t, path, owner, "a", "c")
	newLeft, err := NewReservedClaim(left.Address(), owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	newRight, err := NewReservedClaim(right.Address(), owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	change := mustDocumentChange(t, []Claim{left, right}, []Claim{newLeft, newRight})
	foreignLeft := documentTestClaim(t, path, foreign, "a", "b")
	diagnosticOwner := mustAuthority(t, owner.StatefileKey(), filepath.Join(root, "alias.toml"))
	diagnosticLeft := documentTestClaim(t, path, diagnosticOwner, "a", "b")

	for _, test := range []struct {
		name   string
		claims []Claim
	}{
		{name: "missing", claims: nil},
		{name: "partial", claims: []Claim{left}},
		{name: "mixed", claims: []Claim{left, newRight}},
		{name: "foreign", claims: []Claim{foreignLeft, right}},
		{name: "changed provenance", claims: []Claim{diagnosticLeft, right}},
		{name: "foreign ancestor", claims: []Claim{documentTestClaim(t, path, foreign, "a")}},
		{name: "extra descendant", claims: []Claim{documentTestClaim(t, path, owner, "a", "b", "x"), right}},
	} {
		t.Run(test.name, func(t *testing.T) {
			initial, err := NewRegistry(test.claims)
			if err != nil {
				t.Fatal(err)
			}
			result, changed, err := mustDocumentConvergence(t, nil, change).Apply(initial)
			var stale *StaleClaimError
			if !errors.As(err, &stale) || changed {
				t.Fatalf("unexpected stale-fragment result: changed=%t, error=%v", changed, err)
			}
			if _, _, err := mustClaimConvergence(t, nil).Apply(result); err == nil {
				t.Fatal("stale fragment exposed a usable partial successor")
			}
		})
	}
}

func TestDocumentClaimCoverUsesOnlyInputUnitsForGeneralKeys(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	path := filepath.Join(root, "settings.json")
	before := []Claim{
		documentTestClaim(t, path, owner, "a", "b"),
		documentTestClaim(t, path, owner, "a!"),
		documentTestClaim(t, path, owner, "a", "c"),
		documentTestClaim(t, path, owner, "", "child"),
		documentTestClaim(t, path, owner, "a/b"),
		documentTestClaim(t, path, owner, "~"),
		documentTestClaim(t, path, owner, "\x00"),
	}
	after := []Claim{
		documentTestClaim(t, path, owner, "a"),
		documentTestClaim(t, path, owner, ""),
	}
	change := mustDocumentChange(t, before, after)
	cover := change.FootprintCover()
	if len(cover) != 6 {
		t.Fatalf("cover has %d units, want six exact input units", len(cover))
	}
	for _, claim := range append(slices.Clone(before), after...) {
		if !slices.ContainsFunc(cover, func(address ManagedAddress) bool { return address.Footprint().Contains(claim.Address().Footprint()) }) {
			t.Fatalf("cover omitted input footprint %v", claim.Address())
		}
	}
	for _, address := range cover {
		if !slices.ContainsFunc(append(slices.Clone(before), after...), func(claim Claim) bool { return claim.Address().Equal(address) }) {
			t.Fatal("cover synthesized a wider common ancestor")
		}
	}

	for _, values := range [][]Claim{before, after} {
		slices.Reverse(values)
	}
	permuted := mustDocumentChange(t, before, after)
	if !slices.Equal(cover, permuted.FootprintCover()) || !claimsEqual(change.Expected(), permuted.Expected()) {
		t.Fatal("input permutation changed canonical fragments or cover")
	}
	before[0], after[0] = Claim{}, Claim{}
	change.Expected()[0], change.Target()[0], cover[0] = Claim{}, Claim{}, ManagedAddress{}
	if change.Expected()[0].Validate() != nil || change.Target()[0].Validate() != nil || change.FootprintCover()[0].Validate() != nil {
		t.Fatal("caller mutation changed immutable claim fragments")
	}

	whole := documentTestClaim(t, path, owner)
	wholeChange := mustDocumentChange(t, change.Expected(), []Claim{whole})
	if cover := wholeChange.FootprintCover(); len(cover) != 1 || !cover[0].WholePath() {
		t.Fatal("whole-document input did not cover all key units")
	}
	deep, err := output.ParseKeyPath(strings.Repeat("/", 16384))
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(deep)
	if err != nil {
		t.Fatal(err)
	}
	address, err := NewManagedAddress(mustPathAuthority(t, path), footprint)
	if err != nil {
		t.Fatal(err)
	}
	deepClaim, err := NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	deepChange := mustDocumentChange(t, []Claim{deepClaim}, []Claim{documentTestClaim(t, path, owner, "")})
	initial, err := NewRegistry([]Claim{deepClaim})
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := mustDocumentConvergence(t, nil, deepChange).Apply(initial); err != nil || !changed {
		t.Fatalf("deep empty-segment key did not converge: %v", err)
	}
}

func TestDocumentClaimChangeRejectsInvalidAndOverlappingComponents(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	path := filepath.Join(root, "settings.json")
	left := documentTestClaim(t, path, owner, "a")
	right := documentTestClaim(t, path, owner, "b")
	child := documentTestClaim(t, path, owner, "a", "child")
	otherDocument := documentTestClaim(t, filepath.Join(root, "other.json"), owner, "b")
	otherOwner := documentTestClaim(t, path, mustAuthority(t, filepath.Join(root, "other-state.json"), filepath.Join(root, "daem.toml")), "b")
	for _, test := range []struct{ before, after []Claim }{
		{},
		{before: []Claim{{}}},
		{before: []Claim{left, left}},
		{before: []Claim{left, child}},
		{before: []Claim{left}, after: []Claim{otherDocument}},
		{before: []Claim{left}, after: []Claim{otherOwner}},
	} {
		if result, err := NewDocumentClaimChange(test.before, test.after); err == nil || result.Validate() == nil {
			t.Fatal("invalid document change returned a usable value")
		}
	}
	change := mustDocumentChange(t, []Claim{left}, []Claim{child})
	independent := []ClaimChange{mustClaimChange(t, left.Address(), NoClaim(), NoClaim())}
	if _, err := mustClaimConvergence(t, independent).WithDocumentChanges([]DocumentClaimChange{change}); err == nil {
		t.Fatal("mixed row/document overlap was accepted")
	}
	if _, err := mustClaimConvergence(t, nil).WithDocumentChanges([]DocumentClaimChange{change, mustDocumentChange(t, nil, []Claim{right})}); err == nil {
		t.Fatal("two changes to the same document were accepted")
	}
	if _, err := (ClaimConvergence{}).WithDocumentChanges(nil); err == nil {
		t.Fatal("zero convergence accepted document changes")
	}
	if _, err := mustClaimConvergence(t, nil).WithDocumentChanges([]DocumentClaimChange{{}}); err == nil {
		t.Fatal("zero document change was accepted")
	}
}

func TestDocumentConvergenceComposesDisjointRowsAndDocumentsAtomically(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	alpha := documentTestClaim(t, filepath.Join(root, "alpha.json"), owner, "a")
	alphaChild := documentTestClaim(t, alpha.Address().Path(), owner, "a", "child")
	zeta := documentTestClaim(t, filepath.Join(root, "zeta.json"), owner, "z")
	independent := documentTestClaim(t, filepath.Join(root, "independent"), owner)
	independentValue, _ := PresentClaim(independent)
	rows := []ClaimChange{mustClaimChange(t, independent.Address(), NoClaim(), independentValue)}
	changes := []DocumentClaimChange{
		mustDocumentChange(t, []Claim{alpha}, []Claim{alphaChild}),
		mustDocumentChange(t, []Claim{zeta}, nil),
	}
	initial, err := NewRegistry([]Claim{alpha, zeta})
	if err != nil {
		t.Fatal(err)
	}
	want, err := NewRegistry([]Claim{alphaChild, independent})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]DocumentClaimChange{changes, {changes[1], changes[0]}} {
		convergence := mustDocumentConvergence(t, rows, order...)
		next, changed, err := convergence.Apply(initial)
		if err != nil || !changed || !next.Equal(want) {
			t.Fatalf("mixed components did not converge: %v", err)
		}
		removals := convergence.ExpectedRemovals()
		if !claimsEqual(removals, []Claim{alpha, zeta}) {
			t.Fatal("reshaped and retired claims are not canonical expected removals")
		}
		staleZeta, err := NewReservedClaim(zeta.Address(), owner, "different-operation")
		if err != nil {
			t.Fatal(err)
		}
		stale, err := NewRegistry([]Claim{alpha, staleZeta})
		if err != nil {
			t.Fatal(err)
		}
		result, changed, err := convergence.Apply(stale)
		if err == nil || changed {
			t.Fatal("later stale document did not refuse the complete successor")
		}
		if _, _, err := mustClaimConvergence(t, nil).Apply(result); err == nil {
			t.Fatal("later stale document exposed earlier row/document updates")
		}
	}
	reserved, err := NewReservedClaim(alpha.Address(), owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	promotion := mustDocumentConvergence(t, nil, mustDocumentChange(t, []Claim{reserved}, []Claim{alpha}))
	if len(promotion.ExpectedRemovals()) != 0 {
		t.Fatal("same-address promotion was misclassified as an address removal")
	}
}

func documentTestClaim(t *testing.T, path string, owner stateauthority.Authority, keys ...string) Claim {
	t.Helper()
	footprint := output.WholePathFootprint()
	if len(keys) != 0 {
		key, err := output.NewKeyPath(keys...)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err = output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
	}
	address, err := NewManagedAddress(mustPathAuthority(t, path), footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func mustDocumentChange(t *testing.T, expected, target []Claim) DocumentClaimChange {
	t.Helper()
	change, err := NewDocumentClaimChange(expected, target)
	if err != nil {
		t.Fatal(err)
	}
	return change
}

func mustDocumentConvergence(t *testing.T, rows []ClaimChange, changes ...DocumentClaimChange) ClaimConvergence {
	t.Helper()
	convergence, err := mustClaimConvergence(t, rows).WithDocumentChanges(changes)
	if err != nil {
		t.Fatal(err)
	}
	return convergence
}

func TestDocumentClaimChangeRequiresExactNamespaceAndRejectsExtraCoveredRows(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	path := filepath.Join(strings.ToLower(root), "settings.json")
	parent := documentTestClaim(t, path, owner, "a")
	child := documentTestClaim(t, path, owner, "a", "b")
	otherNamespace, err := pathauthority.NewExact(path, "windows-fold-v1:")
	if err != nil {
		t.Fatal(err)
	}
	otherAddress, err := NewManagedAddress(otherNamespace, child.Address().Footprint())
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewActiveClaim(otherAddress, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDocumentClaimChange([]Claim{parent}, []Claim{other}); err == nil {
		t.Fatal("same lexical path with changed namespace evidence was admitted")
	}
	foreign := mustAuthority(t, filepath.Join(root, "foreign.json"), filepath.Join(root, "foreign.toml"))
	extra := documentTestClaim(t, path, foreign, "a", "c")
	registry, err := NewRegistry([]Claim{child, extra})
	if err != nil {
		t.Fatal(err)
	}
	convergence := mustDocumentConvergence(t, nil, mustDocumentChange(t, []Claim{child}, []Claim{parent}))
	result, changed, err := convergence.Apply(registry)
	var stale *StaleClaimError
	if !errors.As(err, &stale) || changed {
		t.Fatalf("extra covered foreign row was not refused: %v", err)
	}
	if _, _, err := mustClaimConvergence(t, nil).Apply(result); err == nil {
		t.Fatal("foreign descendant refusal exposed a usable registry")
	}
}
