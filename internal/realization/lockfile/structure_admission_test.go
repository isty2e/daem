package lockfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declarationartifact"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

const maximumRejectedLockfileStructureAllocationBytes = 256 << 10

func TestCurrentLockfileRejectsOverLimitTOMLStructure(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    error
	}{
		{
			name:    "depth",
			content: currentLockfileWithNestedInlineTables(tomlstrict.MaximumDepth),
			want:    tomlstrict.ErrMaximumDepthExceeded,
		},
		{
			name:    "containers",
			content: currentLockfileWithRepeatedTables(25_000),
			want:    tomlstrict.ErrMaximumContainersExceeded,
		},
		{
			name:    "work",
			content: currentLockfileWithDenseArrayValues(250_000),
			want:    tomlstrict.ErrMaximumWorkExceeded,
		},
		{
			name:    "path work",
			content: currentLockfileWithLongAncestorSiblings(512),
			want:    tomlstrict.ErrMaximumPathWorkExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateReplacementContent(t.Context(), test.content); !errors.Is(err, test.want) {
				t.Fatalf("ValidateReplacementContent error = %v, want %v", err, test.want)
			}
			if _, err := Load(t.Context(), writeLockfileText(t, string(test.content))); !errors.Is(err, test.want) {
				t.Fatalf("Load error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCurrentLockfileAcceptsExactTOMLDepthBeforeSemanticValidation(t *testing.T) {
	err := validateReplacementContent(t.Context(), currentLockfileWithNestedInlineTables(tomlstrict.MaximumDepth-1))
	if errors.Is(err, tomlstrict.ErrMaximumDepthExceeded) {
		t.Fatalf("ValidateReplacementContent exact-depth error = %v", err)
	}
}

func TestLockfileVersionPolicyDoesNotInterpretOpaqueBodies(t *testing.T) {
	legacy := lockfileWithVersionAndNestedInlineTables(3, tomlstrict.MaximumDepth+128)
	if err := validateReplacementContent(t.Context(), legacy); err != nil {
		t.Fatalf("legacy opaque body error = %v", err)
	}

	futureVersion := lock.CurrentVersion + 1
	future := lockfileWithVersionAndNestedInlineTables(futureVersion, tomlstrict.MaximumDepth+128)
	err := validateReplacementContent(t.Context(), future)
	var versionErr UnsupportedVersionError
	if !errors.As(err, &versionErr) || versionErr.Found != futureVersion {
		t.Fatalf("future opaque body error = %v, want UnsupportedVersionError(%d)", err, futureVersion)
	}
}

func TestLockfileStructureAdmissionHonorsLoadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loadContent(ctx, []byte(fmt.Sprintf("version = %d\n", lock.CurrentVersion)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadContent error = %v, want context.Canceled", err)
	}
}

func TestVersionEnvelopeScanHonorsCancellationWithinLeadingComment(t *testing.T) {
	ctx := &lockfileScanCancellationContext{cancelAt: 2}
	content := []byte("#" + strings.Repeat("a", lockfileContextCheckInterval*4) + "\nversion = 6\n")
	_, err := firstSignificantTOMLLine(ctx, content)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("firstSignificantTOMLLine = %v, want context.Canceled", err)
	}
}

func TestCurrentLockfileRejectsDeepTOMLBeforeDecoderAllocation(t *testing.T) {
	content := currentLockfileWithNestedInlineTables(256)
	assertRejectedLockfileAllocationBound(t, content, tomlstrict.ErrMaximumDepthExceeded)
}

func TestCurrentLockfileRejectsDenseWorkBeforeDecoderAllocation(t *testing.T) {
	content := currentLockfileWithDenseArrayValues(250_000)
	assertRejectedLockfileAllocationBound(t, content, tomlstrict.ErrMaximumWorkExceeded)
}

func assertRejectedLockfileAllocationBound(t *testing.T, content []byte, want error) {
	t.Helper()
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			err := validateReplacementContent(b.Context(), content)
			if !errors.Is(err, want) {
				b.Fatalf("validateReplacementContent error = %v, want %v", err, want)
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedLockfileStructureAllocationBytes {
		t.Fatalf(
			"rejected current lockfile allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedLockfileStructureAllocationBytes,
		)
	}
}

func TestMaximumSelectorSkillsAcrossAllTargetsMarshal(t *testing.T) {
	const skillCount = 4_096
	root := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	supportedTargets := target.SupportedTargets()
	subjects := make([]lock.LockedSubjectContract, 0, skillCount*5)
	for index := range skillCount {
		name := fmt.Sprintf("all-target-skill-%05d", index)
		value := desiredtest.Skill(t, desiredskill.Spec{
			Name: name, Source: root, Targets: supportedTargets,
			Scope: target.ScopeProject, InstallMode: desiredskill.InstallModeCopy,
		})
		subjects = append(subjects, directSkillSubjectContract(t, name))
		projections, err := lockrefine.SkillPathProjections(value)
		if err != nil {
			t.Fatalf("SkillPathProjections(%q): %v", name, err)
		}
		subjects = append(subjects, projections...)
	}
	if len(subjects) != skillCount*5 {
		t.Fatalf("all-target subjects = %d, want %d", len(subjects), skillCount*5)
	}

	content, err := Marshal(lockfileWithSubjects(t, subjects...))
	if err != nil {
		t.Fatalf("Marshal(%d all-target skills): %v", skillCount, err)
	}
	if int64(len(content)) > declarationartifact.MaximumBytes {
		t.Fatalf("all-target lockfile bytes = %d, maximum %d", len(content), declarationartifact.MaximumBytes)
	}
}

func TestMarshalRejectsImpossibleLargeSnapshotBeforeEncoding(t *testing.T) {
	const subjectCount = 1_024
	subjects := make([]lock.LockedSubjectContract, 0, subjectCount)
	for index := range subjectCount {
		subjects = append(subjects, directSkillSubjectContract(t, fmt.Sprintf("invalid-skill-%05d", index)))
	}
	file := lockfileWithSubjects(t, subjects...)
	file.Version = 1

	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, err := Marshal(file); err == nil {
				b.Fatal("Marshal accepted invalid snapshot version")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedLockfileStructureAllocationBytes {
		t.Fatalf(
			"invalid large snapshot allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedLockfileStructureAllocationBytes,
		)
	}
}

func TestLoadAndRelockPreAdmissionLargeCurrentLockfile(t *testing.T) {
	const subjectCount = 1_024
	subjects := make([]lock.LockedSubjectContract, 0, subjectCount)
	for index := range subjectCount {
		subjects = append(subjects, directSkillSubjectContract(t, fmt.Sprintf("legacy-skill-%05d", index)))
	}
	content := encodeCurrentLockfileWithoutStructureAdmission(t, lockfileWithSubjects(t, subjects...))

	loaded, err := loadContent(t.Context(), content)
	if err != nil {
		t.Fatalf("loadContent(pre-admission v6): %v", err)
	}
	relocked, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal(pre-admission v6): %v", err)
	}
	if !bytes.Equal(relocked, content) {
		t.Fatal("pre-admission current lockfile did not relock byte-exactly")
	}
}

func encodeCurrentLockfileWithoutStructureAdmission(t *testing.T, file lock.File) []byte {
	t.Helper()
	output := declarationartifact.NewOutputBuffer()
	dto, err := dtoFromSnapshot(file)
	if err != nil {
		t.Fatalf("dtoFromSnapshot: %v", err)
	}
	if err := toml.NewEncoder(&output).Encode(dto); err != nil {
		t.Fatalf("encode pre-admission lockfile: %v", err)
	}
	return append([]byte(nil), output.Bytes()...)
}

type lockfileScanCancellationContext struct {
	calls    int
	cancelAt int
}

func (ctx *lockfileScanCancellationContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *lockfileScanCancellationContext) Done() <-chan struct{}       { return nil }
func (ctx *lockfileScanCancellationContext) Value(any) any               { return nil }
func (ctx *lockfileScanCancellationContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func currentLockfileWithNestedInlineTables(depth int) []byte {
	return lockfileWithVersionAndNestedInlineTables(lock.CurrentVersion, depth)
}

func lockfileWithVersionAndNestedInlineTables(version int, depth int) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "version = %d\nextra = ", version)
	for range depth {
		builder.WriteString("{ k = ")
	}
	builder.WriteByte('1')
	for range depth {
		builder.WriteString(" }")
	}
	builder.WriteByte('\n')
	return []byte(builder.String())
}

func currentLockfileWithRepeatedTables(count int) []byte {
	var builder strings.Builder
	builder.Grow(count*4 + 32)
	fmt.Fprintf(&builder, "version = %d\n", lock.CurrentVersion)
	for range count {
		builder.WriteString("[a]\n")
	}
	return []byte(builder.String())
}

func currentLockfileWithDenseArrayValues(count int) []byte {
	var builder strings.Builder
	builder.Grow(count*2 + 32)
	fmt.Fprintf(&builder, "version = %d\nextra = [", lock.CurrentVersion)
	for index := range count {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteByte('0')
	}
	builder.WriteString("]\n")
	return []byte(builder.String())
}

func currentLockfileWithLongAncestorSiblings(siblings int) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "version = %d\n[", lock.CurrentVersion)
	builder.WriteString(strings.Repeat("a", tomlstrict.MaximumKeyBytes))
	builder.WriteString("]\n")
	for range siblings {
		builder.WriteString("k = 1\n")
	}
	return []byte(builder.String())
}
