package statefile

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestStateCodecRoundTripsAllNativeGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			state := contributionCodecState(t, syntax)
			snapshot := contributionCodecSnapshot(t, state)
			codec := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{state.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
			content, err := codec.Encode(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := codec.Decode(content)
			if err != nil || !decoded.Equal(snapshot) {
				t.Fatalf("scoped native state round trip differs: %v", err)
			}
			reencoded, err := codec.Encode(decoded)
			if err != nil || !bytes.Equal(content, reencoded) {
				t.Fatalf("scoped native state encoding is not deterministic: %v", err)
			}
			if encoded, err := Marshal(snapshot); err != nil || !bytes.Equal(content, encoded) {
				t.Fatalf("default encoding differs from selected encoding: %v", err)
			}
			if decoded, err := Decode(content); err != nil || !decoded.Equal(snapshot) {
				t.Fatalf("default decoding lost native state: %v", err)
			}
		})
	}
}

func TestContributionBoundStateCodecPreservesFixedAndLegacyEncoding(t *testing.T) {
	codec, err := NewCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testV9Snapshot(t)
	plain, err := Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := codec.Encode(snapshot)
	if err != nil || !bytes.Equal(plain, bound) {
		t.Fatalf("explicit fixed admission changed current state bytes: %v", err)
	}
	for _, version := range []int{legacySnapshotVersion, previousSnapshotVersion, snapshotVersion} {
		input := stateVersionFixture(t, snapshot, version)
		defaultState, err := Decode(input)
		if err != nil {
			t.Fatal(err)
		}
		explicitState, err := codec.Decode(input)
		if err != nil || !explicitState.Equal(defaultState) {
			t.Fatalf("explicit admission changed legacy normalization for version%d: %v", version, err)
		}
	}
	if invalid, err := NewCodec(aggregate.ContributionCatalog{}); err == nil || invalid != nil {
		t.Fatal("invalid explicit catalog returned a usable or default codec")
	}
}

func TestContributionBoundStateCodecRefusesUnselectedInvalidLimitedAndCanceledValues(t *testing.T) {
	jsonState := contributionCodecState(t, configdocument.JSON)
	tomlState := contributionCodecState(t, configdocument.TOML)
	jsonID, tomlID := jsonState.Contribution().CodecContractID(), tomlState.Contribution().CodecContractID()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16}
	complete := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{jsonID, tomlID}, limits)
	input, err := complete.Encode(contributionCodecSnapshot(t, jsonState, tomlState))
	if err != nil {
		t.Fatal(err)
	}
	jsonOnly := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{jsonID}, limits)
	if result, err := jsonOnly.Decode(input); err == nil || len(result.ManagedAggregates()) != 0 {
		t.Fatal("unselected later codec yielded a partial snapshot or fallback admission")
	}
	if result, err := jsonOnly.Encode(contributionCodecSnapshot(t, jsonState, tomlState)); err == nil || len(result) != 0 {
		t.Fatal("unselected value yielded partial encoded state")
	}

	canceled, cancel := context.WithCancel(t.Context())
	canceledCodec := contributionStateCodec(t, canceled, []aggregate.CodecContractID{jsonID}, limits)
	cancel()
	small := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{jsonID}, configdocument.Limits{MaximumBytes: 1, MaximumDepth: 1})
	valid, err := jsonOnly.Encode(contributionCodecSnapshot(t, jsonState))
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []durable.SnapshotCodec{canceledCodec, small} {
		if result, err := codec.Encode(contributionCodecSnapshot(t, jsonState)); err == nil || len(result) != 0 {
			t.Fatal("canceled/limited native admission produced state bytes")
		}
		if result, err := codec.Decode(valid); err == nil || len(result.ManagedAggregates()) != 0 {
			t.Fatal("canceled/limited native admission yielded state rows")
		}
	}
	contribution, err := aggregate.NewManagedContribution(jsonState.Contribution().Contract(), "not-a-canonical-native-value")
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := durable.NewManagedAggregateState(jsonState.Subject(), contribution)
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []durable.SnapshotCodec{jsonOnly, Codec{}} {
		if result, err := codec.Encode(contributionCodecSnapshot(t, invalid)); err == nil || len(result) != 0 {
			t.Fatal("malformed native value bypassed contribution validation")
		}
	}
}

func TestContributionBoundStateCodecKeepsStrictFrameAndFrozenLegacyFamily(t *testing.T) {
	state := nativeAggregateState(t, "ordinary")
	codec := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{state.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	valid, err := codec.Encode(contributionCodecSnapshot(t, state))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(valid, &fields); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]json.RawMessage){
		func(fields map[string]json.RawMessage) { delete(fields, "managed_paths") },
		func(fields map[string]json.RawMessage) { fields["managed_paths"] = json.RawMessage("null") },
		func(fields map[string]json.RawMessage) { fields["unknown"] = json.RawMessage("true") },
	} {
		copy := make(map[string]json.RawMessage, len(fields))
		for key, value := range fields {
			copy[key] = value
		}
		mutate(copy)
		content, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := codec.Decode(content); err == nil || len(result.ManagedAggregates()) != 0 {
			t.Fatal("explicit admission weakened strict state framing")
		}
	}
	for _, raw := range [][]byte{
		append(append([]byte(nil), valid...), []byte("\n{}")...),
		append(append([]byte(nil), valid...), bytes.Repeat([]byte(" "), int(maximumStatefileBytes))...),
	} {
		if result, err := codec.Decode(raw); err == nil || len(result.ManagedAggregates()) != 0 {
			t.Fatal("explicit admission bypassed trailing-value or size refusal")
		}
	}

	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(fields["managed_aggregate_contributions"], &rows); err != nil {
		t.Fatal(err)
	}
	legacyPath, err := state.Contribution().LegacyContentPath()
	if err != nil {
		t.Fatal(err)
	}
	delete(rows[0], "key_path")
	rows[0]["content_path"], err = json.Marshal(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	fields["managed_aggregate_contributions"], err = json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{legacySnapshotVersion, previousSnapshotVersion} {
		fields["version"], err = json.Marshal(version)
		if err != nil {
			t.Fatal(err)
		}
		content, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := codec.Decode(content); err == nil || len(result.ManagedAggregates()) != 0 {
			t.Fatal("injected catalog widened frozen legacy native-family admission")
		}
	}
}

func contributionCodecState(t *testing.T, syntax configdocument.Syntax) durable.ManagedAggregateState {
	t.Helper()
	text := `{"value":[null,true,7]}`
	if syntax == configdocument.TOML {
		text = "value=1979-05-27\n"
	} else if syntax != configdocument.JSON {
		text = "// source\n" + text
	}
	parsed, err := configdocument.Parse(t.Context(), []byte(text), syntax, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := parsed.Leaves(t.Context(), 4096)
	if err != nil || len(leaves) != 1 {
		t.Fatalf("native fixture does not have one atomic value: %v", err)
	}
	id, found := aggregate.HostConfigCodecForSyntax(syntax.String())
	if !found {
		t.Fatal("native fixture syntax unavailable")
	}
	root := outputtest.Parse(t, "@pi-agent/extensions/"+syntax.String()+"/config.data")
	key := outputtest.KeyFootprint(t, "", "a/b", "~")
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, key, id)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, leaves[0].Value.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	resource, err := entity.New(entity.KindHostConfig, "native-"+strings.ReplaceAll(syntax.String(), "-", "_"))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := key.Key()
	subject, err := topologyprojection.HostConfigKeySubject(resource, target.TargetPi, target.ScopeGlobal, path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func contributionCodecSnapshot(t *testing.T, states ...durable.ManagedAggregateState) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: states})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func contributionStateCodec(t *testing.T, ctx context.Context, ids []aggregate.CodecContractID, limits configdocument.Limits) durable.SnapshotCodec {
	t.Helper()
	catalog, err := aggregatecodec.ForContributions(ctx, ids, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}
