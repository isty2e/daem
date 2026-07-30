package retirement

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

type layoutInput struct {
	Active   []Identity
	Controls []Control
	Residues []Residue
	Garbage  []Garbage
	Blockers []Blocker
}

func TestValidateControlAdmitsCanonicalRecordAndRegularTemporaries(t *testing.T) {
	for _, phase := range []Phase{PhasePrepared, PhaseFinalizing} {
		t.Run(string(phase), func(t *testing.T) {
			record := mustRecord(t, testOperationID, testFingerprint, phase)
			control := mustControl(
				t, record,
				mustEntry(t, ".daem-tmp-old", EntryRegular, RecordMode, true, 0),
				mustEntry(t, ".daem-tmp-new", EntryRegular, RecordMode, true, MaximumRecordBytes),
			)

			if !control.Record().Identity().equal(record.Identity()) ||
				control.Record().Phase() != phase {
				t.Fatalf("control record = %#v, want %#v", control.Record(), record)
			}
		})
	}
}

func TestValidateControlRejectsMalformedTree(t *testing.T) {
	record := mustRecord(t, testOperationID, testFingerprint, PhasePrepared)
	content, err := Encode(record)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	validDirectory := mustEntry(
		t,
		record.Identity().ControlName(),
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	)
	validRecord := mustEntry(
		t,
		RecordFileName,
		EntryRegular,
		RecordMode,
		true,
		int64(len(content)),
	)

	tests := []struct {
		name      string
		directory EntryEvidence
		children  []EntryEvidence
		content   []byte
	}{
		{
			name:      "control is regular file",
			directory: mustEntry(t, validDirectory.name, EntryRegular, DirectoryMode, true, 0),
			children:  []EntryEvidence{validRecord},
			content:   content,
		},
		{
			name:      "control is symlink",
			directory: mustEntry(t, validDirectory.name, EntrySymlink, DirectoryMode, true, 0),
			children:  []EntryEvidence{validRecord},
			content:   content,
		},
		{
			name:      "control wrong mode",
			directory: mustEntry(t, validDirectory.name, EntryDirectory, 0o755, true, 0),
			children:  []EntryEvidence{validRecord},
			content:   content,
		},
		{
			name:      "control wrong owner",
			directory: mustEntry(t, validDirectory.name, EntryDirectory, DirectoryMode, false, 0),
			children:  []EntryEvidence{validRecord},
			content:   content,
		},
		{
			name:      "missing record",
			directory: validDirectory,
			content:   content,
		},
		{
			name:      "duplicate record",
			directory: validDirectory,
			children:  []EntryEvidence{validRecord, validRecord},
			content:   content,
		},
		{
			name:      "record is symlink",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntrySymlink,
				RecordMode,
				true,
				int64(len(content)),
			)},
			content: content,
		},
		{
			name:      "record is directory",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntryDirectory,
				RecordMode,
				true,
				int64(len(content)),
			)},
			content: content,
		},
		{
			name:      "record is special",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntrySpecial,
				RecordMode,
				true,
				int64(len(content)),
			)},
			content: content,
		},
		{
			name:      "record wrong mode",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntryRegular,
				0o644,
				true,
				int64(len(content)),
			)},
			content: content,
		},
		{
			name:      "record wrong owner",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntryRegular,
				RecordMode,
				false,
				int64(len(content)),
			)},
			content: content,
		},
		{
			name:      "record size mismatch",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntryRegular,
				RecordMode,
				true,
				int64(len(content))+1,
			)},
			content: content,
		},
		{
			name:      "record oversized",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntryRegular,
				RecordMode,
				true,
				MaximumRecordBytes+1,
			)},
			content: content,
		},
		{
			name:      "temporary empty suffix",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-", EntryRegular, RecordMode, true, 0),
			},
			content: content,
		},
		{
			name:      "temporary symlink",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntrySymlink, RecordMode, true, 0),
			},
			content: content,
		},
		{
			name:      "temporary directory",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntryDirectory, RecordMode, true, 0),
			},
			content: content,
		},
		{
			name:      "temporary special",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntrySpecial, RecordMode, true, 0),
			},
			content: content,
		},
		{
			name:      "temporary wrong mode",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntryRegular, 0o644, true, 0),
			},
			content: content,
		},
		{
			name:      "temporary wrong owner",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntryRegular, RecordMode, false, 0),
			},
			content: content,
		},
		{
			name:      "temporary oversized",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntryRegular, RecordMode, true, MaximumRecordBytes+1),
			},
			content: content,
		},
		{
			name:      "duplicate temporary",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, ".daem-tmp-write", EntryRegular, RecordMode, true, 0),
				mustEntry(t, ".daem-tmp-write", EntryRegular, RecordMode, true, 0),
			},
			content: content,
		},
		{
			name:      "unknown child",
			directory: validDirectory,
			children: []EntryEvidence{
				validRecord,
				mustEntry(t, "other.json", EntryRegular, RecordMode, true, 0),
			},
			content: content,
		},
		{
			name: "record identity mismatch",
			directory: mustEntry(
				t,
				mustIdentity(t, "different-operation", testFingerprint).ControlName(),
				EntryDirectory,
				DirectoryMode,
				true,
				0,
			),
			children: []EntryEvidence{validRecord},
			content:  content,
		},
		{
			name:      "record malformed",
			directory: validDirectory,
			children: []EntryEvidence{mustEntry(
				t,
				RecordFileName,
				EntryRegular,
				RecordMode,
				true,
				int64(len(`{"version":1}`)),
			)},
			content: []byte(`{"version":1}`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ValidateControl(ControlEvidence{
				Directory:     test.directory,
				Children:      test.children,
				RecordContent: test.content,
			}); err == nil {
				t.Fatal("ValidateControl succeeded")
			}
		})
	}
}

func TestValidateResidueRequiresPrivateEntryAndIndependentJournalIdentity(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	residue, err := ValidateResidue(mustEntry(
		t,
		identity.ResidueName(),
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	), identity)
	if err != nil {
		t.Fatalf("ValidateResidue returned error: %v", err)
	}
	if residue.name.value != identity.ResidueName() ||
		!residue.journalIdentity.equal(identity) {
		t.Fatalf("residue = %#v, want exact identity %#v", residue, identity)
	}

	other := mustIdentity(t, "different-operation", testFingerprint)
	tests := []struct {
		evidence EntryEvidence
		identity Identity
	}{
		{mustEntry(t, identity.ControlName(), EntryDirectory, DirectoryMode, true, 0), identity},
		{mustEntry(t, identity.GCName(), EntryDirectory, DirectoryMode, true, 0), identity},
		{mustEntry(t, identity.ResidueName(), EntrySymlink, DirectoryMode, true, 0), identity},
		{mustEntry(t, identity.ResidueName(), EntryDirectory, 0o755, true, 0), identity},
		{mustEntry(t, identity.ResidueName(), EntryDirectory, DirectoryMode, false, 0), identity},
		{mustEntry(t, identity.ResidueName(), EntryDirectory, DirectoryMode, true, 0), other},
		{mustEntry(t, ".unrelated", EntryDirectory, DirectoryMode, true, 0), identity},
	}
	for index, test := range tests {
		t.Run(fmt.Sprintf("reject_%d", index), func(t *testing.T) {
			if _, err := ValidateResidue(test.evidence, test.identity); err == nil {
				t.Fatalf("ValidateResidue(%#v) succeeded", test.evidence)
			}
		})
	}
}

func TestValidatePartialResidueCarriesNoIndependentJournalProof(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	residue, err := ValidatePartialResidue(mustEntry(
		t,
		identity.ResidueName(),
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	))
	if err != nil {
		t.Fatalf("ValidatePartialResidue returned error: %v", err)
	}
	if !residue.valid() || residue.proof != residueProofPhysical ||
		residue.journalIdentity.valid() {
		t.Fatalf("partial residue = %#v, want physical evidence only", residue)
	}

	tests := []EntryEvidence{
		mustEntry(t, identity.ControlName(), EntryDirectory, DirectoryMode, true, 0),
		mustEntry(t, identity.ResidueName(), EntrySymlink, DirectoryMode, true, 0),
		mustEntry(t, identity.ResidueName(), EntryDirectory, 0o755, true, 0),
		mustEntry(t, identity.ResidueName(), EntryDirectory, DirectoryMode, false, 0),
	}
	for index, evidence := range tests {
		t.Run(fmt.Sprintf("reject_%d", index), func(t *testing.T) {
			if _, err := ValidatePartialResidue(evidence); err == nil {
				t.Fatalf("ValidatePartialResidue(%#v) succeeded", evidence)
			}
		})
	}
}

func TestValidateGarbageRequiresPrivateGCEntry(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	garbage, err := ValidateGarbage(mustEntry(
		t,
		identity.GCName(),
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	))
	if err != nil {
		t.Fatalf("ValidateGarbage returned error: %v", err)
	}
	if garbage.name.value != identity.GCName() {
		t.Fatalf("garbage name = %q, want %q", garbage.name.value, identity.GCName())
	}

	tests := []EntryEvidence{
		mustEntry(t, identity.ResidueName(), EntryDirectory, DirectoryMode, true, 0),
		mustEntry(t, identity.GCName(), EntrySymlink, DirectoryMode, true, 0),
		mustEntry(t, identity.GCName(), EntryDirectory, 0o755, true, 0),
		mustEntry(t, identity.GCName(), EntryDirectory, DirectoryMode, false, 0),
	}
	for index, evidence := range tests {
		t.Run(fmt.Sprintf("reject_%d", index), func(t *testing.T) {
			if _, err := ValidateGarbage(evidence); err == nil {
				t.Fatalf("ValidateGarbage(%#v) succeeded", evidence)
			}
		})
	}
}

func TestClassifyAdmittedStatesAndCleanupAuthority(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	other := mustIdentity(t, "20260730T120001.000000000Z-apply", testFingerprint)
	prepared := mustControl(t, mustRecord(t, testOperationID, testFingerprint, PhasePrepared))
	finalizing := mustControl(t, mustRecord(t, testOperationID, testFingerprint, PhaseFinalizing))
	residue := mustResidue(t, identity)
	partialResidue := mustPartialResidue(t, identity)
	gc := mustGarbage(t, identity.GCName())
	otherGC := mustGarbage(t, other.GCName())

	tests := []struct {
		name            string
		evidence        layoutInput
		state           State
		hasCleanup      bool
		residuePresent  bool
		requiresAdvance bool
	}{
		{name: "clean", state: StateClean},
		{name: "finalized", evidence: layoutInput{Garbage: []Garbage{gc}}, state: StateFinalized},
		{
			name:     "active",
			evidence: layoutInput{Active: []Identity{identity}},
			state:    StateActive,
		},
		{
			name: "active with unrelated GC",
			evidence: layoutInput{
				Active:  []Identity{identity},
				Garbage: []Garbage{otherGC},
			},
			state: StateActive,
		},
		{
			name: "prepared active",
			evidence: layoutInput{
				Active:   []Identity{identity},
				Controls: []Control{prepared},
			},
			state: StatePrepared,
		},
		{
			name: "retained",
			evidence: layoutInput{
				Controls: []Control{prepared},
				Residues: []Residue{residue},
			},
			state:           StateRetained,
			hasCleanup:      true,
			residuePresent:  true,
			requiresAdvance: true,
		},
		{
			name: "retained with unrelated GC",
			evidence: layoutInput{
				Controls: []Control{prepared},
				Residues: []Residue{residue},
				Garbage:  []Garbage{otherGC},
			},
			state:           StateRetained,
			hasCleanup:      true,
			residuePresent:  true,
			requiresAdvance: true,
		},
		{
			name: "finalizing partial residue",
			evidence: layoutInput{
				Controls: []Control{finalizing},
				Residues: []Residue{partialResidue},
			},
			state:          StateFinalizing,
			hasCleanup:     true,
			residuePresent: true,
		},
		{
			name: "finalizing absent residue",
			evidence: layoutInput{
				Controls: []Control{finalizing},
			},
			state:      StateFinalizing,
			hasCleanup: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Classify(completeLayout(test.evidence))
			if decision.State() != test.state {
				t.Fatalf("decision = state %q detail=%q, want %q", decision.State(), decision.Detail(), test.state)
			}
			plan, hasCleanup := decision.CleanupPlan()
			if hasCleanup != test.hasCleanup {
				t.Fatalf("CleanupPlan present = %t, want %t", hasCleanup, test.hasCleanup)
			}
			if !hasCleanup {
				return
			}
			if plan.Classification() != ClassificationRetainedCleanupResidue {
				t.Fatalf("classification = %q", plan.Classification())
			}
			if plan.Action() != ActionFinalizeJournalCleanup {
				t.Fatalf("action = %q", plan.Action())
			}
			authority := plan.Authority()
			if authority.OperationID() != testOperationID ||
				authority.JournalAuthorityFingerprint() != testFingerprint ||
				authority.ControlName() != identity.ControlName() ||
				authority.ResidueName() != identity.ResidueName() ||
				authority.GCName() != identity.GCName() {
				t.Fatalf("cleanup authority = %#v, want exact identity %#v", authority, identity)
			}
			if authority.ResiduePresent() != test.residuePresent {
				t.Fatalf("ResiduePresent = %t, want %t", authority.ResiduePresent(), test.residuePresent)
			}
			if authority.RequiresPhaseAdvance() != test.requiresAdvance {
				t.Fatalf("RequiresPhaseAdvance = %t, want %t", authority.RequiresPhaseAdvance(), test.requiresAdvance)
			}
			currentRecord, err := authority.CurrentRecord()
			if err != nil {
				t.Fatalf("CurrentRecord returned error: %v", err)
			}
			finalizingRecord, err := currentRecord.Finalizing()
			if err != nil {
				t.Fatalf("Record.Finalizing returned error: %v", err)
			}
			if finalizingRecord.Phase() != PhaseFinalizing ||
				!finalizingRecord.Identity().equal(identity) {
				t.Fatalf("finalizing record = %#v", finalizingRecord)
			}
		})
	}
}

func TestClassifyBlocksUnadmittedStateCrossProducts(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	other := mustIdentity(t, "20260730T120001.000000000Z-apply", testFingerprint)
	prepared := mustControl(t, mustRecord(t, testOperationID, testFingerprint, PhasePrepared))
	finalizing := mustControl(t, mustRecord(t, testOperationID, testFingerprint, PhaseFinalizing))
	otherPrepared := mustControl(t, mustRecord(
		t,
		other.OperationID(),
		other.JournalAuthorityFingerprint(),
		PhasePrepared,
	))
	residue := mustResidue(t, identity)
	partialResidue := mustPartialResidue(t, identity)
	otherResidue := mustResidue(t, other)
	gc := mustGarbage(t, identity.GCName())

	tests := []struct {
		name     string
		evidence layoutInput
	}{
		{
			name:     "multiple active",
			evidence: layoutInput{Active: []Identity{identity, other}},
		},
		{
			name:     "multiple controls",
			evidence: layoutInput{Controls: []Control{prepared, otherPrepared}},
		},
		{
			name:     "multiple residues",
			evidence: layoutInput{Residues: []Residue{residue, otherResidue}},
		},
		{
			name: "active control mismatch",
			evidence: layoutInput{
				Active:   []Identity{identity},
				Controls: []Control{otherPrepared},
			},
		},
		{
			name: "active finalizing control",
			evidence: layoutInput{
				Active:   []Identity{identity},
				Controls: []Control{finalizing},
			},
		},
		{
			name: "active residue",
			evidence: layoutInput{
				Active:   []Identity{identity},
				Residues: []Residue{residue},
			},
		},
		{
			name: "active control and residue",
			evidence: layoutInput{
				Active:   []Identity{identity},
				Controls: []Control{prepared},
				Residues: []Residue{residue},
			},
		},
		{
			name:     "prepared control without residue",
			evidence: layoutInput{Controls: []Control{prepared}},
		},
		{
			name: "prepared control with partial residue",
			evidence: layoutInput{
				Controls: []Control{prepared},
				Residues: []Residue{partialResidue},
			},
		},
		{
			name: "prepared control residue mismatch",
			evidence: layoutInput{
				Controls: []Control{prepared},
				Residues: []Residue{otherResidue},
			},
		},
		{
			name:     "residue without control",
			evidence: layoutInput{Residues: []Residue{residue}},
		},
		{
			name: "control and matching GC",
			evidence: layoutInput{
				Controls: []Control{finalizing},
				Garbage:  []Garbage{gc},
			},
		},
		{
			name: "residue and matching GC",
			evidence: layoutInput{
				Residues: []Residue{residue},
				Garbage:  []Garbage{gc},
			},
		},
		{
			name: "duplicate artifact",
			evidence: layoutInput{
				Garbage: []Garbage{gc, gc},
			},
		},
		{
			name: "uninitialized active",
			evidence: layoutInput{
				Active: []Identity{{}},
			},
		},
		{
			name: "uninitialized control",
			evidence: layoutInput{
				Controls: []Control{{}},
			},
		},
		{
			name: "uninitialized artifact",
			evidence: layoutInput{
				Residues: []Residue{{}},
			},
		},
		{
			name: "uninitialized GC",
			evidence: layoutInput{
				Garbage: []Garbage{{}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Classify(completeLayout(test.evidence))
			if decision.State() != StateBlocked ||
				strings.TrimSpace(decision.Detail()) == "" {
				t.Fatalf("decision = state %q detail=%q, want blocked diagnostic", decision.State(), decision.Detail())
			}
			if _, ok := decision.CleanupPlan(); ok {
				t.Fatal("blocked decision exposed cleanup authority")
			}
		})
	}
}

func TestBlockerForNameFailsClosedOnlyForReservedEvidence(t *testing.T) {
	legacy := InspectName(".daem-tombstone-orphan")
	blocker, ok := BlockerForName(legacy)
	if !ok || !strings.Contains(blocker.detail, "manual remediation") {
		t.Fatalf("legacy blocker = %#v, %t", blocker, ok)
	}

	malformed := InspectName("retirement-v1-short")
	if blocker, ok = BlockerForName(malformed); !ok || !strings.Contains(blocker.detail, "malformed") {
		t.Fatalf("malformed blocker = %#v, %t", blocker, ok)
	}

	unrelated := InspectName(".private-build-residue")
	if blocker, ok = BlockerForName(unrelated); ok {
		t.Fatalf("unrelated hidden name produced blocker %#v", blocker)
	}
}

func TestClassifyUsesDeterministicBlockerDetail(t *testing.T) {
	zeta, err := NewBlocker("zeta", "zeta failure")
	if err != nil {
		t.Fatalf("NewBlocker(zeta) returned error: %v", err)
	}
	alpha, err := NewBlocker("alpha", "alpha failure")
	if err != nil {
		t.Fatalf("NewBlocker(alpha) returned error: %v", err)
	}
	decision := Classify(completeLayout(layoutInput{Blockers: []Blocker{zeta, alpha}}))
	if decision.State() != StateBlocked || decision.Detail() != "alpha failure" {
		t.Fatalf("decision = %#v, want deterministic alpha blocker", decision)
	}
}

func TestZeroAndForgedDecisionsFailClosed(t *testing.T) {
	incomplete := Classify(LayoutEvidence{})
	if incomplete.State() != StateBlocked || !strings.Contains(incomplete.Detail(), "incomplete") {
		t.Fatalf("incomplete layout decision = %#v, want fail-closed diagnostic", incomplete)
	}

	var zero Decision
	if zero.State() != StateBlocked ||
		!strings.Contains(zero.Detail(), "uninitialized") {
		t.Fatalf(
			"zero decision = state %q detail=%q",
			zero.State(),
			zero.Detail(),
		)
	}
	if _, ok := zero.CleanupPlan(); ok {
		t.Fatal("zero decision exposed cleanup plan")
	}

	var zeroPlan CleanupPlan
	if zeroPlan.Classification() != "" || zeroPlan.Action() != "" {
		t.Fatalf(
			"zero cleanup plan exposed classification %q action %q",
			zeroPlan.Classification(),
			zeroPlan.Action(),
		)
	}
	if _, err := zeroPlan.Authority().CurrentRecord(); err == nil {
		t.Fatal("zero cleanup authority exposed a current record")
	}

	forgedGarbage := Garbage{name: Name{
		value:  ".daem-journal-gc-v1-" + testDigest,
		kind:   NameGC,
		digest: "short",
	}}
	decision := Classify(completeLayout(layoutInput{Garbage: []Garbage{forgedGarbage}}))
	if decision.State() != StateBlocked {
		t.Fatalf("forged GC decision = %#v, want blocked", decision)
	}
}

func TestLayoutEvidenceOwnsBoundarySlices(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	active := []Identity{identity}
	evidence := NewLayoutEvidence(active, nil, nil, nil, nil)
	active[0] = Identity{}

	decision := Classify(evidence)
	if decision.State() != StateActive {
		t.Fatalf("decision after caller mutation = %#v, want stable active state", decision)
	}
}

func TestEntryEvidenceRejectsUnsafeOrContradictoryFacts(t *testing.T) {
	tests := []struct {
		name string
		kind EntryKind
		mode fs.FileMode
		size int64
	}{
		{name: "", kind: EntryRegular, mode: RecordMode},
		{name: ".", kind: EntryRegular, mode: RecordMode},
		{name: "..", kind: EntryRegular, mode: RecordMode},
		{name: "nested/name", kind: EntryRegular, mode: RecordMode},
		{name: "nul\x00name", kind: EntryRegular, mode: RecordMode},
		{name: "entry", kind: EntryKind("unknown"), mode: RecordMode},
		{name: "entry", kind: EntryRegular, mode: fs.ModeSymlink | RecordMode},
		{name: "entry", kind: EntryRegular, mode: RecordMode, size: -1},
	}
	for index, test := range tests {
		t.Run(fmt.Sprintf("reject_%d", index), func(t *testing.T) {
			if _, err := NewEntryEvidence(test.name, test.kind, test.mode, true, test.size); err == nil {
				t.Fatalf("NewEntryEvidence(%q, %q, %v, %d) succeeded", test.name, test.kind, test.mode, test.size)
			}
		})
	}
}

func mustEntry(
	t *testing.T,
	name string,
	kind EntryKind,
	mode fs.FileMode,
	owned bool,
	size int64,
) EntryEvidence {
	t.Helper()

	evidence, err := NewEntryEvidence(name, kind, mode, owned, size)
	if err != nil {
		t.Fatalf("NewEntryEvidence(%q) returned error: %v", name, err)
	}
	return evidence
}

func completeLayout(input layoutInput) LayoutEvidence {
	return NewLayoutEvidence(
		input.Active,
		input.Controls,
		input.Residues,
		input.Garbage,
		input.Blockers,
	)
}

func mustControl(t *testing.T, record Record, temporaries ...EntryEvidence) Control {
	t.Helper()

	content, err := Encode(record)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	children := []EntryEvidence{mustEntry(
		t,
		RecordFileName,
		EntryRegular,
		RecordMode,
		true,
		int64(len(content)),
	)}
	children = append(children, temporaries...)
	control, err := ValidateControl(ControlEvidence{
		Directory: mustEntry(
			t,
			record.Identity().ControlName(),
			EntryDirectory,
			DirectoryMode,
			true,
			0,
		),
		Children:      children,
		RecordContent: content,
	})
	if err != nil {
		t.Fatalf("ValidateControl returned error: %v", err)
	}
	return control
}

func mustResidue(t *testing.T, identity Identity) Residue {
	t.Helper()

	residue, err := ValidateResidue(mustEntry(
		t,
		identity.ResidueName(),
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	), identity)
	if err != nil {
		t.Fatalf("ValidateResidue returned error: %v", err)
	}
	return residue
}

func mustPartialResidue(t *testing.T, identity Identity) Residue {
	t.Helper()

	residue, err := ValidatePartialResidue(mustEntry(
		t,
		identity.ResidueName(),
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	))
	if err != nil {
		t.Fatalf("ValidatePartialResidue returned error: %v", err)
	}
	return residue
}

func mustGarbage(t *testing.T, name string) Garbage {
	t.Helper()

	garbage, err := ValidateGarbage(mustEntry(
		t,
		name,
		EntryDirectory,
		DirectoryMode,
		true,
		0,
	))
	if err != nil {
		t.Fatalf("ValidateGarbage returned error: %v", err)
	}
	return garbage
}
