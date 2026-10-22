package apply

import (
	"slices"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestPiSourceFingerprintsAreDeterministicAndBindWarnings(t *testing.T) {
	planned := applyAuthorityTestPlan(t)
	selection, err := targetselection.ForDiagnostics([]string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := diagnose.PiPackageSourceDiagnostics([]desiredextension.Extension{
		piFingerprintExtension(t, "first", "github:acme/tool"),
		piFingerprintExtension(t, "second", "git+https://router.home.arpa/acme/private.git"),
		piFingerprintExtension(t, "third", "git@router.home.arpa:acme/private.git"),
	}, selection)
	if err != nil || len(diagnostics) != 3 {
		t.Fatalf("diagnostics=%#v error=%v", diagnostics, err)
	}
	withWarnings := planned
	withWarnings.result.Diagnostics = diagnostics
	full, err := applyOperationFingerprint(withWarnings, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := providerStableFingerprint(withWarnings, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}

	for _, rows := range [][]findings.Diagnostic{diagnostics, {diagnostics[2], diagnostics[0], diagnostics[1]}} {
		repeated := planned
		repeated.result.Diagnostics = rows
		gotFull, err := applyOperationFingerprint(repeated, reconcile.ContextApply)
		if err != nil {
			t.Fatal(err)
		}
		gotStable, err := providerStableFingerprint(repeated, reconcile.ContextApply)
		if err != nil || !full.Equal(gotFull) || !stable.Equal(gotStable) {
			t.Fatalf("repeated/reordered warning changed operation evidence: %v", err)
		}
	}

	changed := withWarnings
	changed.result.Diagnostics = slices.Clone(diagnostics)
	changed.result.Diagnostics[2].Detail += "; changed fixed policy"
	withoutSCP := withWarnings
	withoutSCP.result.Diagnostics = diagnostics[:2]
	for _, variant := range []commandPlan{planned, changed, withoutSCP} {
		gotFull, err := applyOperationFingerprint(variant, reconcile.ContextApply)
		if err != nil {
			t.Fatal(err)
		}
		gotStable, err := providerStableFingerprint(variant, reconcile.ContextApply)
		if err != nil || full.Equal(gotFull) || stable.Equal(gotStable) {
			t.Fatalf("warning presence/detail was not bound into operation evidence: %v", err)
		}
	}
}

func piFingerprintExtension(t *testing.T, name, ref string) desiredextension.Extension {
	t.Helper()
	source, err := desiredextension.NewAuthoredSourceRef(desiredextension.SourceKindHostSource, ref)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name: name, Carrier: desiredextension.CarrierPiPackage, Target: target.TargetPi,
		Scope: target.ScopeProject, Source: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
