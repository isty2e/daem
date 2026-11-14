package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestDocumentBindingTransferSettlesOneKeyAndReplaysWithoutChangingOtherFacts(t *testing.T) {
	root := t.TempDir()
	before := testDocumentBinding(t, root, "owner", "~/.claude.json", filepath.Join(root, "old.json"))
	target := mustPathAuthority(t, filepath.Join(root, "current.json"))
	intent, err := NewExactDocumentBindingTransferIntent(before, target)
	if err != nil {
		t.Fatal(err)
	}
	afterValue, known := intent.KnownAfter()
	after, present := afterValue.Get()
	if !known || !present || !after.PathAuthority().Equal(target) || !after.Owner().ExactEqual(before.Owner()) || after.Destination() != before.Destination() || !intent.Before().Equal(mustBindingValue(t, before)) {
		t.Fatal("transfer changed its logical key, before evidence or stored provenance")
	}
	if _, err := NewExactDocumentBindingIntent(before.Owner(), before.Destination(), intent.Before(), afterValue); err == nil {
		t.Fatal("ordinary intent acquired implicit namespace-retarget permission")
	}
	if _, err := NewExactDocumentBindingTransferIntent(before, before.PathAuthority()); err == nil {
		t.Fatal("same-namespace retention was presented as a transfer")
	}
	if _, err := NewExactDocumentBindingTransferIntent(before, pathtest.DarwinCaseSensitive(before.PathAuthority().Key())); err == nil {
		t.Fatal("filesystem-semantics drift at one key was presented as a transfer")
	}
	if changes, err := intent.ChangesBinding(); err != nil || !changes {
		t.Fatal("exact transfer did not report binding work", err)
	}
	if _, err := NewExactDocumentBindingTransferIntent(DocumentBinding{}, target); err == nil {
		t.Fatal("transfer without an exact source was admitted")
	}
	if _, err := NewExactDocumentBindingTransferIntent(before, pathauthority.Exact{}); err == nil {
		t.Fatal("transfer without an exact target was admitted")
	}

	foreign := testDocumentBinding(t, root, "foreign", "~/.claude.json", before.PathAuthority().Key())
	claim, err := NewActiveClaim(mustAddress(t, before.PathAuthority().Key(), ""), foreign.Owner())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry([]Claim{claim}, before, foreign)
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := NewDocumentBindingConvergence([]DocumentBindingIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	want, err := NewRegistry([]Claim{claim}, after, foreign)
	if err != nil {
		t.Fatal(err)
	}
	settled, changed, err := convergence.Apply(registry)
	if err != nil || !changed || !settled.Equal(want) {
		t.Fatal("transfer did not settle one binding while retaining foreign facts", err)
	}
	replayed, changed, err := convergence.Apply(settled)
	if err != nil || changed || !replayed.Equal(want) {
		t.Fatal("transfer replay changed its already-settled binding", err)
	}
	stale := testDocumentBinding(t, root, "owner", "~/.claude.json", filepath.Join(root, "unexpected.json"))
	third, err := NewRegistry([]Claim{claim}, stale, foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := convergence.Apply(third); err == nil || changed {
		t.Fatal("transfer overwrote a third namespace", err)
	}
	retire, err := NewExactDocumentBindingIntent(before.Owner(), before.Destination(), intent.Before(), NoDocumentBinding())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDocumentBindingConvergence([]DocumentBindingIntent{intent, retire}); err == nil {
		t.Fatal("transfer and independent retirement both claimed the same binding key")
	}
}

func TestDocumentBindingTransferPromotesItsPendingTargetWithoutLosingTheSource(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "current")
	candidate := filepath.Join(namespace, "Caf\u00e9.json")
	provisional, err := pathauthority.NewProvisional(candidate, pathtest.DarwinCaseSensitive(candidate).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
	if err != nil {
		t.Fatal(err)
	}
	before := testDocumentBinding(t, root, "owner", "~/.claude.json", filepath.Join(root, "old.json"))
	pending, err := NewProvisionalDocumentBindingTransferIntent(before, provisional)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := pending.KnownAfter(); known {
		t.Fatal("pending transfer became a known absent or exact target")
	}
	if value, ok := pending.ProvisionalAfter(); !ok || !value.Equal(provisional) || !pending.Before().Equal(mustBindingValue(t, before)) {
		t.Fatal("pending transfer lost its target comparison or exact source")
	}
	if _, err := NewDocumentBindingConvergence([]DocumentBindingIntent{pending}); err == nil {
		t.Fatal("unpromoted transfer entered exact registry settlement")
	}
	observed := pathtest.DarwinCaseSensitive(filepath.Join(namespace, "Cafe\u0301.json"))
	promoted, err := pending.Promote(observed)
	if err != nil {
		t.Fatal(err)
	}
	want, err := NewExactDocumentBindingTransferIntent(before, observed)
	if err != nil || !promoted.Equal(want) {
		t.Fatal("promotion changed transfer semantics or source provenance", err)
	}
	if _, ok := pending.ProvisionalAfter(); !ok {
		t.Fatal("promotion mutated its pending input")
	}
	if _, err := pending.Promote(pathtest.DarwinCaseSensitive(filepath.Join(root, "elsewhere.json"))); err == nil {
		t.Fatal("transfer promoted a target outside its namespace")
	}

	sameSource, err := NewDocumentBinding(before.Owner(), before.Destination(), observed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvisionalDocumentBindingTransferIntent(sameSource, provisional); err == nil {
		t.Fatal("pending transfer admitted possible normalization onto its source")
	}
	sameKey := testDocumentBinding(t, root, "owner", "~/.claude.json", candidate)
	if _, err := NewProvisionalDocumentBindingTransferIntent(sameKey, provisional); err == nil {
		t.Fatal("pending transfer admitted its source key with changed filesystem semantics")
	}
	if changes, err := pending.ChangesBinding(); err != nil || !changes {
		t.Fatal("pending transfer did not report binding work", err)
	}

	confined, err := pathauthority.NewProvisional(candidate, provisional.CandidateWitness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	separate, err := NewDocumentBinding(before.Owner(), before.Destination(), pathtest.DarwinCaseSensitive(filepath.Join(root, "legacy", "settings.json")))
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := NewProvisionalDocumentBindingTransferIntent(separate, confined)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guarded.Promote(separate.PathAuthority()); err == nil {
		t.Fatal("promotion accepted the source instead of the freshly selected target")
	}
	if _, err := NewProvisionalDocumentBindingTransferIntent(before, pathauthority.Provisional{}); err == nil {
		t.Fatal("pending transfer accepted missing target evidence")
	}
}
