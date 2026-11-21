package apply

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func TestReplacementTokenMatchesLegacyPlan(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	codexHome := filepath.Join(root, "codex")
	t.Setenv("CODEX_HOME", codexHome)
	manifest := filepath.Join(workspace, "daem.toml")
	source := filepath.Join(workspace, "preferences.toml")
	writeApplyFile(t, manifest, fmt.Sprintf("version=1\ntargets=['codex']\n[[agent_config]]\nid='preferences'\nscope='global'\npath='config.toml'\nsource=%q\n", source))
	writeApplyFile(t, source, "model='managed'\nnotify=true\n")
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	initial, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteWithOptions(t.Context(), initial, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	writeApplyFile(t, filepath.Join(codexHome, "config.toml"), "model='local'\nnotify=true\n")

	plan, err := PlanNativeReplacement(t.Context(), NativeReplacementInput{ManifestPath: manifest, ID: "preferences", Keys: []string{"/model"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plan.Close() })
	planned := plan.prepared.lifecycle.planned
	if !slices.ContainsFunc(planned.assessment.NativeApproval, func(group journalobservation.ApprovalEvidence) bool {
		return len(group.Roles) != 0 && len(group.GlobalLocations) != 0
	}) {
		t.Fatal("fixture needs a selected global document with a host role")
	}
	legacy := legacyReplacementBytes(t, planned)
	if token := fmt.Sprintf("v1:%x", sha256.Sum256(legacy)); plan.Disclosure().Fingerprint != token {
		t.Fatal("replacement token differs from the legacy payload")
	}
	if !plan.prepared.lifecycle.operationEvidence.Equal(mutation.NewOperationFingerprint(legacy)) {
		t.Fatal("prepared identity differs from the disclosed projection")
	}
	planned.assessment.NativeApproval = nil
	want := legacyReplacementBytes(t, planned)
	got, err := nativeReplacementApprovalPayload(planned)
	if err != nil || !bytes.Equal(want, got) || bytes.Equal(want, legacy) {
		t.Fatal("replacement without protection did not retain its distinct legacy bytes", err)
	}
}

func legacyReplacementBytes(t *testing.T, planned commandPlan) []byte {
	t.Helper()
	planned.assessment.GlobalCarrierClaims = durablecarrier.EmptyGlobalCarrierClaims()
	identity, err := applyOperationIdentity(planned, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}
	observed := planned.assessment.NativeSources[0].SourceContract()
	supply, _ := observed.ExactSupply()
	placement, _ := observed.AgentConfigPlacement()
	selected, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	type source struct{ Subject, SourceID, ResolvedRef, ContentHash, Profile, Grammar string }
	type root struct{ Identity, Policy string }
	type role struct {
		Root                               root
		Target, Scope, Path, Child, Policy string
	}
	type member struct{ Subject, Target, Scope, Path, Footprint, Key, Codec string }
	type protection struct {
		Namespace  []ownershipObservationFingerprintFacts
		Protection string
		Roles      []role
		Global     []root
		Members    []member
	}
	var protections []protection
	for _, evidence := range planned.assessment.NativeApproval {
		if !slices.Contains(evidence.Aliases, selected) {
			continue
		}
		p := protection{Protection: evidence.Protection}
		for _, alias := range evidence.Aliases {
			fact := ownershipObservationFingerprintFacts{Destination: alias.AggregateRoot().String(), FootprintKind: output.WholePathFootprint().Kind().String()}
			if exact, present := evidence.Authority.Exact(); present {
				fact.AuthorityKind = "exact"
				fact.PathAuthority = pathAuthorityFingerprintFactsFor(exact)
			} else {
				missing, _ := evidence.Authority.Provisional()
				fact.AuthorityKind = "provisional"
				fact.ProvisionalCandidateKey = missing.CandidateKey()
				fact.ProvisionalWitness = missing.CandidateWitness()
				fact.ProvisionalNamespaceKey = missing.Namespace().Key()
				fact.ProvisionalNamespaceWitness = missing.Namespace().Witness()
			}
			p.Namespace = append(p.Namespace, fact)
		}
		for _, r := range evidence.Roles {
			child, _ := r.Coordinate().DirectoryChild()
			ref := r.RootReference()
			p.Roles = append(p.Roles, role{root{ref.Identity(), ref.LocatorPolicy()}, string(r.Target()), string(r.Scope()), r.Destination().String(), child, r.PolicyID()})
		}
		for _, r := range evidence.GlobalLocations {
			p.Global = append(p.Global, root{r.Identity(), r.LocatorPolicy()})
		}
		for _, row := range planned.assessment.CurrentState.ManagedAggregates() {
			contribution := row.Contribution()
			address := contribution.Address()
			doc := address.Document()
			if !slices.Contains(evidence.Aliases, doc) {
				continue
			}
			key, _ := address.Footprint().Key()
			p.Members = append(p.Members, member{row.Subject().String(), string(doc.Target()), string(doc.Scope()), doc.AggregateRoot().String(), address.Footprint().Kind().String(), key.String(), string(contribution.CodecContractID())})
		}
		protections = append(protections, p)
	}
	payload, err := json.Marshal(struct {
		Operation  string
		Identity   operationplan.ApplyIdentityInput
		Source     source
		Protection []protection
		Limits     configdocument.Limits
	}{"replace-agent-config/v1", identity, source{observed.SubjectID().String(), string(supply.SourceID()), string(supply.ResolvedRef()), string(supply.ContentHash()), placement.ID(), string(placement.Grammar())}, protections, nativeconfig.DocumentLimits()})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
