package lockfile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/realization/lock"
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
			content: currentLockfileWithTables(tomlstrict.MaximumContainers + 1),
			want:    tomlstrict.ErrMaximumContainersExceeded,
		},
		{
			name:    "work",
			content: currentLockfileWithArrayValues(tomlstrict.MaximumWork),
			want:    tomlstrict.ErrMaximumWorkExceeded,
		},
		{
			name:    "path work",
			content: currentLockfileWithLongAncestorSiblings(tomlstrict.MaximumPathWork/tomlstrict.MaximumKeyBytes + 1),
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

func TestCurrentLockfileRejectsDeepTOMLBeforeDecoderAllocation(t *testing.T) {
	content := currentLockfileWithNestedInlineTables(256)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if err := validateReplacementContent(b.Context(), content); err == nil {
				b.Fatal("validateReplacementContent accepted over-depth current lockfile")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedLockfileStructureAllocationBytes {
		t.Fatalf(
			"over-depth current lockfile allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedLockfileStructureAllocationBytes,
		)
	}
}

func TestMarshalRejectsStructurallyOversizedCanonicalLockfile(t *testing.T) {
	const subjectCount = 5000
	subjects := make([]lock.LockedSubjectContract, 0, subjectCount)
	for index := range subjectCount {
		subjects = append(subjects, directSkillSubjectContract(t, fmt.Sprintf("skill-%05d", index)))
	}
	_, err := Marshal(lockfileWithSubjects(t, subjects...))
	if !lockfileStructureLimitError(err) {
		t.Fatalf("Marshal error = %v, want TOML structure limit", err)
	}
}

func lockfileStructureLimitError(err error) bool {
	return errors.Is(err, tomlstrict.ErrMaximumDepthExceeded) ||
		errors.Is(err, tomlstrict.ErrMaximumContainersExceeded) ||
		errors.Is(err, tomlstrict.ErrMaximumWorkExceeded) ||
		errors.Is(err, tomlstrict.ErrMaximumPathWorkExceeded)
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

func currentLockfileWithTables(count int) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "version = %d\n", lock.CurrentVersion)
	for index := range count {
		fmt.Fprintf(&builder, "[extra%d]\n", index)
	}
	return []byte(builder.String())
}

func currentLockfileWithArrayValues(count int) []byte {
	var builder strings.Builder
	builder.Grow(count*3 + 32)
	fmt.Fprintf(&builder, "version = %d\nextra = [", lock.CurrentVersion)
	for index := range count {
		if index > 0 {
			builder.WriteString(", ")
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
