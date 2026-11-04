package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func journalWithVersion(t *testing.T, content []byte, version int) []byte {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["version"] = json.RawMessage(fmt.Sprint(version))
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func nativeAcquireTransition(t *testing.T, root string, keys ...string) ownershipmutation.ClaimTransition {
	t.Helper()
	key, err := output.NewKeyPath(keys...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(pathtest.Exact(filepath.Join(root, "config.json")), footprint)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(address, owner, "native-acquire")
	if err != nil {
		t.Fatal(err)
	}
	return transition
}

func TestRecoveryClaimFootprintsRoundTripWithoutLegacyInterpretation(t *testing.T) {
	root := t.TempDir()
	var transitions []ownershipmutation.ClaimTransition
	for _, key := range [][]string{{""}, {"a/b"}, {"a~1b"}, {"a", "b"}, {"日本語", "line\nkey"}} {
		transitions = append(transitions, nativeAcquireTransition(t, root, key...))
	}
	records, err := recoveryClaimTransitions(transitions)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []recoveryClaimTransition
	if err := decodeRecoveryJSONStrict(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalClaimTransitions(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != len(transitions) {
		t.Fatal("recovery dropped ownership transitions")
	}
	for _, expected := range transitions {
		found := false
		for _, actual := range canonical {
			found = found || actual.Equal(expected)
		}
		if !found {
			t.Fatalf("recovery changed native ownership %v", expected.Address().Footprint())
		}
	}

	parent := nativeAcquireTransition(t, root, "")
	child := nativeAcquireTransition(t, root, "", "nested")
	overlapping, err := recoveryClaimTransitions([]ownershipmutation.ClaimTransition{child, parent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalClaimTransitions(overlapping); err == nil {
		t.Fatal("recovery admitted overlapping native key ownership")
	}
}

func TestRecoveryClaimFootprintsRefuseMissingAndAmbiguousScope(t *testing.T) {
	transition := nativeAcquireTransition(t, t.TempDir(), "")
	record, err := recoveryClaimValueFrom(transition.Prepared())
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	for _, footprint := range []*footprintcodec.Record{
		nil,
		{},
		{Kind: "key", KeyPath: &empty},
		{Kind: "path", KeyPath: &empty},
	} {
		invalid := record
		invalid.Footprint = footprint
		if _, err := canonicalClaimValue(invalid); err == nil {
			t.Fatalf("invalid recovery scope admitted: %#v", footprint)
		}
	}
	if _, err := canonicalClaimValue(recoveryClaimValue{Footprint: record.Footprint}); err == nil {
		t.Fatal("absent claim retained authority fields")
	}

	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	fields["content_path"] = json.RawMessage(`"/legacy"`)
	ambiguous, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var decoded recoveryClaimValue
	if err := decodeRecoveryJSONStrict(ambiguous, &decoded); err == nil {
		t.Fatal("new recovery schema accepted a competing legacy scope")
	}
}

func TestRecoveryJournalRefusesV13AndRetainsItsEvidence(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	legacy := journalWithVersion(t, content, 13)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, recoveryJournalFileName)

	if err := os.WriteFile(path, legacy, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}
	_, err = loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "use the daem version that wrote it") {
		t.Fatalf("old journal did not require recovery by its writing version: %v", err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || string(retained) != string(legacy) {
		t.Fatalf("version refusal changed recovery evidence: %v", err)
	}
}
