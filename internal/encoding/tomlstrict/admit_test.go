package tomlstrict

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAdmitAcceptsExactDepthLimit(t *testing.T) {
	content := nestedInlineTables(MaximumDepth)
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(depth=%d) = %v, want nil", MaximumDepth, err)
	}
}

func TestAdmitRejectsDepthLimitPlusOne(t *testing.T) {
	content := nestedInlineTables(MaximumDepth + 1)
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("Admit(depth=%d) = %v, want depth exceeded", MaximumDepth+1, err)
	}
}

func TestAdmitAcceptsExactContainerLimit(t *testing.T) {
	limits := Limits{MaximumDepth: 8, MaximumContainers: 3, MaximumWork: 64}
	content := "[a]\n[b]\n[c]\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(containers=3) = %v, want nil", err)
	}
}

func TestAdmitRejectsContainerLimitPlusOne(t *testing.T) {
	limits := Limits{MaximumDepth: 8, MaximumContainers: 3, MaximumWork: 64}
	content := "[a]\n[b]\n[c]\n[d]\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumContainersExceeded) {
		t.Fatalf("Admit(containers=4) = %v, want container exceeded", err)
	}
}

func TestAdmitAcceptsExactWorkLimit(t *testing.T) {
	limits := Limits{MaximumDepth: 8, MaximumContainers: 8, MaximumWork: 4}
	// key (1) + array enter (1) + two primitives (2) = 4
	if err := Admit(context.Background(), []byte("k = [1, 2]\n"), limits); err != nil {
		t.Fatalf("Admit(work=4) = %v, want nil", err)
	}
}

func TestAdmitRejectsWorkLimitPlusOne(t *testing.T) {
	limits := Limits{MaximumDepth: 8, MaximumContainers: 8, MaximumWork: 4}
	err := Admit(context.Background(), []byte("k = [1, 2, 3]\n"), limits)
	if !errors.Is(err, ErrMaximumWorkExceeded) {
		t.Fatalf("Admit(work=5) = %v, want work exceeded", err)
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
