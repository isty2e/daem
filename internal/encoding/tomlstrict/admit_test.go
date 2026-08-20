package tomlstrict

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAdmitAcceptsExactSemanticPathOfNestedInlineTables(t *testing.T) {
	// N inline tables under root key k yield path length N+1 at the innermost
	// assignment, so MaximumDepth maps to N=MaximumDepth-1.
	content := nestedInlineTables(MaximumDepth - 1)
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(path=%d) = %v, want nil", MaximumDepth, err)
	}
}

func TestAdmitRejectsNestedInlineTablesBeyondSemanticPath(t *testing.T) {
	content := nestedInlineTables(MaximumDepth)
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(path=%d) = %v, want depth exceeded", MaximumDepth+1, err)
	}
}

func TestAdmitAcceptsExactContainerDepthOfNestedArrays(t *testing.T) {
	content := nestedArrays(MaximumDepth)
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(arrays=%d) = %v, want nil", MaximumDepth, err)
	}
}

func TestAdmitRejectsNestedArraysBeyondContainerDepth(t *testing.T) {
	content := nestedArrays(MaximumDepth + 1)
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(arrays=%d) = %v, want depth exceeded", MaximumDepth+1, err)
	}
}

func TestAdmitAcceptsExactContainerLimit(t *testing.T) {
	limits := testLimits(8, 3, 64, 64)
	content := "[a]\n[b]\n[c]\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(containers=3) = %v, want nil", err)
	}
}

func TestAdmitRejectsContainerLimitPlusOne(t *testing.T) {
	limits := testLimits(8, 3, 64, 64)
	content := "[a]\n[b]\n[c]\n[d]\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumContainersExceeded) {
		t.Fatalf("Admit(containers=4) = %v, want container exceeded", err)
	}
}

func TestAdmitAcceptsExactWorkLimit(t *testing.T) {
	limits := testLimits(8, 8, 5, 64)
	// key tokens (1) + root walk (1) + array enter (1) + two primitives (2) = 5
	if err := Admit(context.Background(), []byte("k = [1, 2]\n"), limits); err != nil {
		t.Fatalf("Admit(work=5) = %v, want nil", err)
	}
}

func TestAdmitRejectsWorkLimitPlusOne(t *testing.T) {
	limits := testLimits(8, 8, 5, 64)
	err := Admit(context.Background(), []byte("k = [1, 2, 3]\n"), limits)
	if !errors.Is(err, ErrMaximumWorkExceeded) {
		t.Fatalf("Admit(work=6) = %v, want work exceeded", err)
	}
}

func TestAdmitIgnoresBracesInsideStrings(t *testing.T) {
	content := "k = \"{ k = { k = 1 } }\"\n"
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(string braces) = %v, want nil", err)
	}
}

func TestAdmitRejectsDeepInlineTablesWithoutDecoding(t *testing.T) {
	content := nestedInlineTables(5000)
	if got := utf8.RuneCountInString(content); got > 40_000 {
		t.Fatalf("fixture size = %d runes, want a small nested document", got)
	}
	started := time.Now()
	err := Admit(context.Background(), []byte(content), StandardLimits())
	elapsed := time.Since(started)
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(depth=5000) = %v, want depth exceeded", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Admit(depth=5000) took %s, want fail-closed before Unmarshal cost", elapsed)
	}
}

func TestAdmitHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Admit(ctx, []byte("k = 1\n"), StandardLimits())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Admit(canceled) = %v, want context.Canceled", err)
	}
}

func TestAdmitRejectsNonPositiveLimits(t *testing.T) {
	err := Admit(context.Background(), []byte("k = 1\n"), Limits{})
	if err == nil {
		t.Fatal("Admit(zero limits) = nil, want error")
	}
}

func TestAdmitAcceptsExactSemanticPathInDottedInlineTable(t *testing.T) {
	// Outer key contributes path 1; 63-part inner key reaches MaximumDepth.
	content := "k = { " + dottedKey(MaximumDepth-1) + " = 1 }\n"
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(path=%d) = %v, want nil", MaximumDepth, err)
	}
}

func TestAdmitRejectsSemanticPathLimitPlusOneInDottedInlineTable(t *testing.T) {
	content := "k = { " + dottedKey(MaximumDepth) + " = 1 }\n"
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(path=%d) = %v, want depth exceeded", MaximumDepth+1, err)
	}
}

func TestAdmitRejectsNestedDottedInlineTablesBeforeDecode(t *testing.T) {
	content := nestedDottedInlineTables(MaximumDepth, MaximumDepth)
	if got := utf8.RuneCountInString(content); got > 80_000 {
		t.Fatalf("fixture size = %d runes, want a small nested dotted document", got)
	}
	started := time.Now()
	err := Admit(context.Background(), []byte(content), StandardLimits())
	elapsed := time.Since(started)
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(nested dotted path) = %v, want depth exceeded", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Admit(nested dotted path) took %s, want fail-closed before Unmarshal cost", elapsed)
	}
}

func TestAdmitAcceptsExactSemanticPathFromHeaderAndAssignment(t *testing.T) {
	headerParts := MaximumDepth / 2
	assignmentParts := MaximumDepth - headerParts
	content := "[" + dottedKey(headerParts) + "]\n" + dottedKey(assignmentParts) + " = 1\n"
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(header+assignment path=%d) = %v, want nil", MaximumDepth, err)
	}
}

func TestAdmitRejectsHeaderPlusAssignmentBeyondSemanticPath(t *testing.T) {
	headerParts := MaximumDepth / 2
	assignmentParts := MaximumDepth - headerParts + 1
	content := "[" + dottedKey(headerParts) + "]\n" + dottedKey(assignmentParts) + " = 1\n"
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(header+assignment path=%d) = %v, want depth exceeded", MaximumDepth+1, err)
	}
}

func TestAdmitCountsQuotedDottedTextAsOneKeyPart(t *testing.T) {
	content := "k = { \"a.b.c.d.e\" = 1 }\n"
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(quoted dotted key) = %v, want nil", err)
	}
}

func TestAdmitAcceptsExactKeyByteLimit(t *testing.T) {
	content := strings.Repeat("a", MaximumKeyBytes) + " = 1\n"
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(keyBytes=%d) = %v, want nil", MaximumKeyBytes, err)
	}
}

func TestAdmitRejectsKeyByteLimitPlusOne(t *testing.T) {
	content := strings.Repeat("a", MaximumKeyBytes+1) + " = 1\n"
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumWorkExceeded) {
		t.Fatalf("Admit(keyBytes=%d) = %v, want work exceeded", MaximumKeyBytes+1, err)
	}
}

func TestAdmitAcceptsSpaceSeparatedDatetimes(t *testing.T) {
	tests := []string{
		"checked_at = 1987-07-05 17:45:00Z\n",
		"space = 1987-07-05 17:45:00Z\n",
		"lower = 1987-07-05t17:45:00z\n",
		"odt4 = 1979-05-27 07:32:00Z\n",
		"local = 1987-07-05 17:45:00\n",
		"frac = 1979-05-27 00:32:00.999-07:00\n",
		"commented = 1987-07-05 17:45:00Z # keep\n",
		"inline = { checked_at = 1987-07-05 17:45:00Z }\n",
		"list = [1987-07-05 17:45:00Z]\n",
	}
	for _, content := range tests {
		if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
			t.Fatalf("Admit(%q) = %v, want nil", content, err)
		}
	}
}

func TestAdmitStillTerminatesNonDatetimeBareValuesOnSpace(t *testing.T) {
	err := Admit(context.Background(), []byte("k = 1 2\n"), StandardLimits())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("Admit(spaced integers) = %v, want malformed assignment", err)
	}
}

func TestAdmitRejectsDottedKeyInsideArrayOfInlineTablesBeyondPath(t *testing.T) {
	content := "k = [{ " + dottedKey(MaximumDepth) + " = 1 }]\n"
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(array dotted path) = %v, want depth exceeded", err)
	}
}

func testLimits(depth int, containers int, work int, keyBytes int) Limits {
	return Limits{
		MaximumDepth:      depth,
		MaximumContainers: containers,
		MaximumWork:       work,
		MaximumKeyBytes:   keyBytes,
	}
}

func nestedInlineTables(depth int) string {
	var builder strings.Builder
	builder.WriteString("k = ")
	for range depth {
		builder.WriteString("{k = ")
	}
	builder.WriteByte('1')
	for range depth {
		builder.WriteByte('}')
	}
	builder.WriteByte('\n')
	return builder.String()
}

func nestedArrays(depth int) string {
	var builder strings.Builder
	builder.WriteString("k = ")
	for range depth {
		builder.WriteByte('[')
	}
	builder.WriteByte('1')
	for range depth {
		builder.WriteByte(']')
	}
	builder.WriteByte('\n')
	return builder.String()
}

func nestedDottedInlineTables(levels int, parts int) string {
	var builder strings.Builder
	builder.WriteString("k = ")
	key := dottedKey(parts)
	for range levels {
		builder.WriteByte('{')
		builder.WriteString(key)
		builder.WriteString(" = ")
	}
	builder.WriteByte('1')
	for range levels {
		builder.WriteByte('}')
	}
	builder.WriteByte('\n')
	return builder.String()
}

func dottedKey(parts int) string {
	var builder strings.Builder
	for index := range parts {
		if index > 0 {
			builder.WriteByte('.')
		}
		builder.WriteByte('a')
		builder.WriteString(strconv.Itoa(index))
	}
	return builder.String()
}
