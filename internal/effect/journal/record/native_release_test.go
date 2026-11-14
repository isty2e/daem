package record

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output/ownership"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func nativeReleaseFixture(t *testing.T, claimless, bindingAbsent bool) (NativeReleaseInput, durable.SnapshotCodec) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	row := fixture.before.ManagedAggregates()[0]
	id, _, present := topologyprojection.HostConfigKeyIdentity(row.Subject())
	if !present {
		t.Fatal("native fixture has no host-config identity")
	}
	identities, err := journaldocument.HostConfigManagements(t.Context(), fixture.before, id)
	if err != nil || len(identities) != 1 {
		t.Fatal(identities, err)
	}
	after, err := identities[0].RemoveFrom(t.Context(), fixture.before)
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, afterBytes, err := EncodeSnapshots(fixture.before, after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := NewNativeReleaseExactNamespace(fixture.claims.exact)
	if err != nil {
		t.Fatal(err)
	}
	input := NativeReleaseInput{
		OperationID: "release-native-config", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Owner: fixture.claims.owner, Before: fixture.before, Management: identities[0], Namespace: namespace,
	}
	if !claimless {
		input.OwnClaimsBefore = []ownership.Claim{documentActiveClaim(t, fixture.claims, row.Contribution().Footprint())}
	}
	if !bindingAbsent {
		input.BindingBefore = membershipBindingValue(t, fixture.claims)
	}
	for _, role := range []NativeReleaseFileRole{NativeReleaseManifest, NativeReleaseDefaultLock, NativeReleaseState} {
		path := fixture.claims.owner.ManifestPath()
		if role == NativeReleaseDefaultLock {
			path = filepath.Join(filepath.Dir(path), "daem.lock.json")
		}
		target := releaseTargetForTest(t, path)
		if role == NativeReleaseState {
			target, err = NewNativeReleaseExactNamespace(fixture.claims.owner.StatefileAuthority())
			if err != nil {
				t.Fatal(err)
			}
		}
		fileInput := NativeReleaseFileInput{Role: role, Target: target}
		if role == NativeReleaseState {
			fileInput.BeforeExists, fileInput.Write, fileInput.BeforeMode = true, true, 0o600
			fileInput.BeforeSHA256, fileInput.AfterSHA256 = Fingerprint(beforeBytes), Fingerprint(afterBytes)
		}
		file, err := NewNativeReleaseFileIntent(fileInput)
		if err != nil {
			t.Fatal(err)
		}
		input.Files = append(input.Files, file)
	}
	return input, fixture.codec
}

func TestNativeReleaseRoundTripKeepsASeparateClosedPurposeAndCodec(t *testing.T) {
	for _, claimless := range []bool{false, true} {
		for _, bindingAbsent := range []bool{false, true} {
			input, codec := nativeReleaseFixture(t, claimless, bindingAbsent)
			release, err := NewNativeRelease(t.Context(), input, codec)
			if err != nil {
				t.Fatal(err)
			}
			content, err := release.Encode(t.Context(), codec)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := DecodeNativeSnapshot(t.Context(), privateJournalSnapshotForTest(t, content), "journal.json", codec)
			if err != nil || !loaded.Initialized() {
				t.Fatal("release did not load as its own family", err)
			}
			decoded, ok := loaded.NativeRelease()
			if !ok || decoded.Before().Equal(decoded.After()) || !decoded.Before().Equal(input.Before) || !decoded.Management().Equal(input.Management) {
				t.Fatal("release collapsed complete state or selected identity")
			}
			if _, ok := loaded.Native(); ok {
				t.Fatal("release acquired a host-mutation projection")
			}
			if _, ok := loaded.Ordinary(); ok {
				t.Fatal("release acquired an ordinary projection")
			}
			if loaded.OperationID() != input.OperationID {
				t.Fatal("release operation identity changed")
			}
			caller := &rejectingFingerprintCodec{}
			fingerprint, err := loaded.AuthorityFingerprint(t.Context(), caller)
			if err != nil || caller.calls != 0 || fingerprint != Fingerprint(content) {
				t.Fatal("release lost codec custody", err)
			}
			encoded, err := decoded.Encode(t.Context(), codec)
			if err != nil || !bytes.Equal(encoded, content) {
				t.Fatal("release round trip changed canonical bytes", err)
			}
			var bindings []ownership.DocumentBinding
			if binding, present := input.BindingBefore.Get(); present {
				bindings = append(bindings, binding)
			}
			registry, err := ownership.NewRegistry(input.OwnClaimsBefore, bindings...)
			if err != nil {
				t.Fatal(err)
			}
			if err := decoded.Metadata().RequireBefore(registry); err != nil {
				t.Fatal(err)
			}
			after, _, err := decoded.Metadata().ApplyAfterCommit(registry)
			if err != nil || !after.Equal(ownership.EmptyRegistry()) {
				t.Fatal("scoped release did not remove its own metadata", err)
			}
		}
	}
}

func TestNativeReleaseDecoderRefusesMixedPayloadEqualStateAndForgedDeltas(t *testing.T) {
	input, codec := nativeReleaseFixture(t, false, false)
	release, err := NewNativeRelease(t.Context(), input, codec)
	if err != nil {
		t.Fatal(err)
	}
	content, err := release.Encode(t.Context(), codec)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]json.RawMessage){
		"host-entries": func(body map[string]json.RawMessage) { body["entries"] = json.RawMessage("[]") },
		"host-groups":  func(body map[string]json.RawMessage) { body["document_groups"] = json.RawMessage("[]") },
		"equal-state":  func(body map[string]json.RawMessage) { body["statefile_after"] = body["statefile_before"] },
		"unrelated-state": func(body map[string]json.RawMessage) {
			body["statefile_before"], body["statefile_after"] = body["statefile_after"], body["statefile_before"]
		},
		"missing-claim-absence": func(body map[string]json.RawMessage) { delete(body, "own_claims_before") },
		"null-claim-absence":    func(body map[string]json.RawMessage) { body["own_claims_before"] = json.RawMessage("null") },
		"omit-own-removal":      func(body map[string]json.RawMessage) { body["own_claims_removed"] = json.RawMessage("[]") },
		"wrong-purpose":         func(body map[string]json.RawMessage) { body["purpose"] = json.RawMessage(`"host-mutation"`) },
		"other-operation":       func(body map[string]json.RawMessage) { body["operation"] = json.RawMessage(`"apply"`) },
	} {
		t.Run(name, func(t *testing.T) {
			var body map[string]json.RawMessage
			if err := json.Unmarshal(content, &body); err != nil {
				t.Fatal(err)
			}
			mutate(body)
			changed, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := DecodeNativeSnapshot(t.Context(), privateJournalSnapshotForTest(t, changed), "journal.json", codec)
			if err == nil || loaded.Initialized() {
				t.Fatal("mixed, incomplete or forged release was admitted")
			}
		})
	}
}

func TestNativeReleaseRefusesEqualSnapshotConstructionAndCanceledOutput(t *testing.T) {
	input, codec := nativeReleaseFixture(t, true, true)
	input.Before = durable.EmptySnapshot()
	if value, err := NewNativeRelease(t.Context(), input, codec); err == nil || value.Validate() == nil {
		t.Fatal("equal Snapshot construction admitted a release")
	}
	input, codec = nativeReleaseFixture(t, true, true)
	release, err := NewNativeRelease(t.Context(), input, codec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if value, err := NewNativeRelease(ctx, input, codec); !errors.Is(err, context.Canceled) || value.Validate() == nil {
		t.Fatal("canceled construction admitted a release", err)
	}
	if content, err := release.Encode(ctx, codec); !errors.Is(err, context.Canceled) || content != nil {
		t.Fatal("canceled encoding exposed a usable journal", err)
	}
}

func TestNativeReleaseMatchesTheSelectedFilesNotOnlyTheirRolesAndImages(t *testing.T) {
	input, codec := nativeReleaseFixture(t, false, false)
	release, err := NewNativeRelease(t.Context(), input, codec)
	if err != nil {
		t.Fatal(err)
	}
	files := release.LocalFiles()
	if err := release.MatchLocalTargets(files[0].Target(), files[1].Target(), files[2].Target()); err != nil {
		t.Fatal(err)
	}
	otherManifest := releaseTargetForTest(t, filepath.Join(filepath.Dir(input.Owner.ManifestPath()), "same-content-other.toml"))
	if err := release.MatchLocalTargets(otherManifest, files[1].Target(), files[2].Target()); err == nil {
		t.Fatal("another manifest in the same state authority acquired the recorded intent")
	}
	if err := release.MatchLocalTargets(files[1].Target(), files[0].Target(), files[2].Target()); err == nil {
		t.Fatal("role permutation acquired a recorded target")
	}
	state := files[2]
	wrongState, err := NewNativeReleaseFileIntent(NativeReleaseFileInput{
		Role: state.Role(), Target: otherManifest,
		BeforeExists: state.BeforeExists(), BeforeSHA256: state.BeforeSHA256(), BeforeMode: state.BeforeMode(),
		AfterSHA256: state.AfterSHA256(), Write: state.Writes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	input.Files[2] = wrongState
	if _, err := NewNativeRelease(t.Context(), input, codec); err == nil {
		t.Fatal("state role escaped the selected owner authority")
	}
}
