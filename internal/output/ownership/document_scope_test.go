package ownership

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestScopedRegistrySeparatesDisjointProtectionFromCompleteEquality(t *testing.T) {
	root := t.TempDir()
	selected := testDocumentBinding(t, root, "selected", ".pi/settings.json", filepath.Join(root, "selected.json"))
	foreign := testDocumentBinding(t, root, "foreign", "@pi-agent/settings.json", filepath.Join(root, "foreign.json"))
	before, err := NewRegistryWithProtections(nil, []DocumentBinding{selected, foreign}, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewRegistryWithProtections(before.Claims(), before.DocumentBindings(), []PendingProtection{testPendingProtection(t, foreign)})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := NewDocumentScope([]pathauthority.Exact{selected.PathAuthority()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := before.Documents(scope)
	right, _ := after.Documents(scope)
	lh, _ := left.Fingerprint()
	rh, _ := right.Fingerprint()
	if before.Equal(after) || !left.Equal(right) || lh != rh || scope.RequireWritable(after) != nil {
		t.Fatal("unrelated protection changed document admission or vanished from complete equality")
	}
	intersecting, err := NewRegistryWithProtections(nil, before.DocumentBindings(), []PendingProtection{testPendingProtection(t, selected)})
	if err != nil {
		t.Fatal(err)
	}
	blocked, _ := intersecting.Documents(scope)
	bh, _ := blocked.Fingerprint()
	var pending *ProtectionPendingError
	if left.Equal(blocked) || lh == bh || !errors.As(scope.RequireWritable(intersecting), &pending) {
		t.Fatal("same-owner protection did not fence ordinary writes")
	}
}

func TestDocumentViewRetainsEntireIntersectingRecordAndItsPhase(t *testing.T) {
	root := t.TempDir()
	a := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "a.json"))
	b := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", filepath.Join(root, "b.json"))
	base := testPendingProtection(t, a)
	first, _ := NewProtectedDocument([]DocumentBinding{a}, 501, 20, 0o640)
	second, _ := NewProtectedDocument([]DocumentBinding{b}, 501, 20, 0o640)
	armed, err := NewArmedProtection(a.Owner(), base.Invocation(), base.Correlation(), []ProtectedDocument{first, second})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := armed.Bind(testProtectionExecution(t, armed.Invocation()))
	if err != nil {
		t.Fatal(err)
	}
	narrowed, _ := NewProtectedDocument([]DocumentBinding{b}, 501, 20, 0o600)
	remoteChange, err := NewArmedProtection(a.Owner(), base.Invocation(), base.Correlation(), []ProtectedDocument{first, narrowed})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := NewDocumentScope([]pathauthority.Exact{a.PathAuthority()}, nil)
	views := make([]DocumentRegistryView, 0, 4)
	for _, pending := range [][]PendingProtection{nil, {armed}, {bound}, {remoteChange}} {
		registry, err := NewRegistryWithProtections(nil, []DocumentBinding{a, b}, pending)
		if err != nil {
			t.Fatal(err)
		}
		view, err := registry.Documents(scope)
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, view)
	}
	if len(views[1].protections) != 1 || !views[1].protections[0].Equal(armed) {
		t.Fatal("projection truncated a multi-document obligation")
	}
	for left := range views {
		for right := 0; right < left; right++ {
			lh, lerr := views[left].Fingerprint()
			rh, rerr := views[right].Fingerprint()
			if lerr != nil || rerr != nil || views[left].Equal(views[right]) || lh == rh {
				t.Fatal("record removal, phase or remote document change disappeared from the view")
			}
		}
	}
}

func TestProvisionalDocumentScopeRetainsNamespaceExclusion(t *testing.T) {
	namespace := filepath.Join(t.TempDir(), "settings")
	candidate := filepath.Join(namespace, "caf\u00e9.json")
	missing, err := pathauthority.NewProvisional(candidate, pathtest.DarwinCaseSensitive(candidate).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
	if err != nil {
		t.Fatal(err)
	}
	scope, err := NewDocumentScope(nil, []pathauthority.Provisional{missing})
	if err != nil {
		t.Fatal(err)
	}
	if !scope.contains(pathtest.DarwinCaseSensitive(filepath.Join(namespace, "cafe\u0301.json"))) ||
		scope.contains(pathtest.DarwinCaseSensitive(filepath.Join(filepath.Dir(namespace), "disjoint.json"))) {
		t.Fatal("provisional history collapsed into empty or unrelated exact scope")
	}
}

func TestDocumentScopeIsCanonicalImmutableAndRejectsZeroViews(t *testing.T) {
	root := t.TempDir()
	a := mustPathAuthority(t, filepath.Join(root, "a"))
	b := mustPathAuthority(t, filepath.Join(root, "b"))
	input := []pathauthority.Exact{b, a, a}
	scope, err := NewDocumentScope(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = pathauthority.Exact{}
	canonical, _ := NewDocumentScope([]pathauthority.Exact{a, b}, nil)
	if !scope.Equal(canonical) {
		t.Fatal("scope order or caller mutation changed its meaning")
	}
	if _, err := EmptyRegistry().Documents(DocumentScope{}); err == nil {
		t.Fatal("zero scope became an empty approval")
	}
	if _, err := (DocumentRegistryView{}).Fingerprint(); err == nil {
		t.Fatal("zero view became a fingerprint")
	}
}
