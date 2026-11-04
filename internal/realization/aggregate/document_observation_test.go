package aggregate_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentObservationBindsWholeBytesAndSelectedState(t *testing.T) {
	codec, selection := documentObservationFixture(t, "json", "@pi-agent/extension-settings/example.json")
	source := []byte(`{"a":1,"z":1,"released":1}`)
	original := bytes.Clone(source)
	document := aggregate.ExistingDocument(source)
	observed, err := aggregate.ObserveDocument(codec, document, selection)
	if err != nil || observed.Validate() != nil {
		t.Fatalf("observe existing document: %v", err)
	}
	want, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	if !observed.Snapshot().Equal(want) || observed.ContentHash() != artifact.HashFileContent(original) {
		t.Fatal("selected state and digest did not describe the supplied document")
	}

	source[5] = '9'
	returned := observed.Snapshot().States()
	returned[0] = aggregate.ProjectionState{}
	if !observed.Snapshot().Equal(want) || observed.ContentHash() != artifact.HashFileContent(original) {
		t.Fatal("caller mutation changed immutable observation")
	}

	changed, err := aggregate.ObserveDocument(codec, aggregate.ExistingDocument([]byte(`{"a":1,"z":1,"released":9}`)), selection)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.Snapshot().Equal(observed.Snapshot()) || changed.ContentHash() == observed.ContentHash() {
		t.Fatal("unselected content was lost from the whole-document digest")
	}
}

func TestDocumentObservationDistinguishesExistingEmptyFromAbsent(t *testing.T) {
	codec, selection := documentObservationFixture(t, "toml", "@pi-agent/extension-settings/example.toml")
	observed, err := aggregate.ObserveDocument(codec, aggregate.ExistingDocument(nil), selection)
	if err != nil || observed.Validate() != nil || !observed.Snapshot().DocumentExisted() ||
		observed.ContentHash() != artifact.HashFileContent(nil) {
		t.Fatalf("existing empty TOML did not produce valid bound evidence: %v", err)
	}
	if observed, err := aggregate.ObserveDocument(codec, aggregate.AbsentDocument(), selection); err == nil || observed.Validate() == nil {
		t.Fatal("absent document produced a positive existing-document observation")
	}
	if (aggregate.DocumentObservation{}).Validate() == nil {
		t.Fatal("zero observation was usable")
	}
}

func TestDocumentObservationHonorsCodecCancellation(t *testing.T) {
	_, selection := documentObservationFixture(t, "json", "@pi-agent/extension-settings/example.json")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(ctx, selection.CodecContractID(), limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	observation, err := aggregate.ObserveDocument(codec, aggregate.ExistingDocument([]byte(`{"a":1,"z":1}`)), selection)
	if err == nil || observation.Validate() == nil {
		t.Fatal("cancelled codec read produced usable observation")
	}
}

func TestDocumentObservationRefusesInvalidInputsAndCodecOutputs(t *testing.T) {
	codec, selection := documentObservationFixture(t, "json", "@pi-agent/extension-settings/example.json")
	otherCodec, _ := documentObservationFixture(t, "toml", "@pi-agent/extension-settings/example.toml")
	_, otherSelection := documentObservationFixture(t, "json", "@pi-agent/extension-settings/other.json")
	document := aggregate.ExistingDocument([]byte(`{"a":1,"z":1}`))
	valid, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	foreign, failure := codec.Read(document, otherSelection)
	if failure != nil {
		t.Fatal(failure)
	}
	absent, failure := codec.Read(aggregate.AbsentDocument(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	refusal, err := aggregate.NewCodecFailure(aggregate.CodecFailureDocumentMalformed, output.KeyPath{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		codec     aggregate.Codec
		document  aggregate.Document
		selection aggregate.Selection
	}{
		{name: "missing codec", document: document, selection: selection},
		{name: "missing selection", codec: codec, document: document},
		{name: "wrong codec", codec: otherCodec, document: document, selection: selection},
		{name: "malformed", codec: codec, document: aggregate.ExistingDocument([]byte("{")), selection: selection},
		{name: "oversized", codec: documentObservationLimitedCodec{codec, 1}, document: document, selection: selection},
		{name: "invalid byte policy", codec: documentObservationLimitedCodec{codec, 0}, document: document, selection: selection},
		{name: "wrong returned presence", codec: documentObservationResultCodec{codec, absent, nil}, document: document, selection: selection},
		{name: "foreign returned selection", codec: documentObservationResultCodec{codec, foreign, nil}, document: document, selection: selection},
		{name: "refused with snapshot", codec: documentObservationResultCodec{codec, valid, refusal}, document: document, selection: selection},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed, err := aggregate.ObserveDocument(test.codec, test.document, test.selection)
			if err == nil || observed.Validate() == nil {
				t.Fatal("invalid read produced usable bound evidence")
			}
		})
	}
}

type documentObservationLimitedCodec struct {
	aggregate.Codec
	maximum int64
}

func (codec documentObservationLimitedCodec) MaximumDocumentBytes() int64 { return codec.maximum }

type documentObservationResultCodec struct {
	aggregate.Codec
	snapshot aggregate.Snapshot
	failure  *aggregate.CodecFailure
}

func (codec documentObservationResultCodec) Read(aggregate.Document, aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	return codec.snapshot, codec.failure
}

func documentObservationFixture(t *testing.T, syntax, destination string) (aggregate.Codec, aggregate.Selection) {
	t.Helper()
	codecID, ok := aggregate.HostConfigCodecForSyntax(syntax)
	if !ok {
		t.Fatal("unknown test syntax")
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(t.Context(), codecID, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	root, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	var contracts []aggregate.ProjectionContract
	for _, name := range []string{"a", "z"} {
		key, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, footprint, codecID)
		if err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, contract)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	return codec, selection
}
