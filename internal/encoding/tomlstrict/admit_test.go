package tomlstrict

import (
	"context"
	"errors"
	"math"
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
	limits := testLimits(8, 3, 64, 64, 64)
	content := "[a]\n[b]\n[c]\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(containers=3) = %v, want nil", err)
	}
}

func TestAdmitRejectsContainerLimitPlusOne(t *testing.T) {
	limits := testLimits(8, 3, 64, 64, 64)
	content := "[a]\n[b]\n[c]\n[d]\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumContainersExceeded) {
		t.Fatalf("Admit(containers=4) = %v, want container exceeded", err)
	}
}

func TestAdmitAcceptsExactWorkLimit(t *testing.T) {
	limits := testLimits(8, 8, 5, 64, 64)
	// key tokens (1) + root walk (1) + array enter (1) + two primitives (2) = 5
	if err := Admit(context.Background(), []byte("k = [1, 2]\n"), limits); err != nil {
		t.Fatalf("Admit(work=5) = %v, want nil", err)
	}
}

func TestAdmitRejectsWorkLimitPlusOne(t *testing.T) {
	limits := testLimits(8, 8, 5, 64, 64)
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

func TestAdmitHonorsCancellationBeforeInvalidUTF8Tail(t *testing.T) {
	content := append([]byte(strings.Repeat("a", cancelCheckInterval*4)), 0xff)
	ctx := &cancelOnErrCallContext{cancelAt: 2}
	err := Admit(ctx, content, StandardLimits())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Admit(canceled UTF-8 scan) = %v, want context.Canceled", err)
	}
}

func TestScannerChecksCancellationWithinLongLexicalRegions(t *testing.T) {
	long := strings.Repeat("a", cancelCheckInterval*4)
	tests := []struct {
		name    string
		content string
	}{
		{name: "whitespace", content: strings.Repeat(" ", cancelCheckInterval*4)},
		{name: "comment", content: "#" + long},
		{name: "basic string", content: "k = \"" + long + "\""},
		{name: "multiline string", content: "k = \"\"\"" + long + "\"\"\""},
		{name: "quoted key", content: "\"" + long + "\" = 1"},
		{name: "bare value", content: "k = " + long},
		{name: "bare key", content: long + " = 1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scan := scanner{src: []byte(test.content), limits: testLimits(64, 1<<20, 1<<20, 1<<20, 1<<20)}
			ctx := &cancelAtScannerIndexContext{scan: &scan, threshold: cancelCheckInterval}
			scan.ctx = ctx
			err := scan.document()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("document() = %v, want context.Canceled", err)
			}
			if ctx.canceledAt > cancelCheckInterval*2 {
				t.Fatalf("first cancellation observation index = %d, want at most %d", ctx.canceledAt, cancelCheckInterval*2)
			}
		})
	}
}

func TestScannerChecksCancellationWithinLongQuoteRun(t *testing.T) {
	scan := scanner{
		src:    []byte("k = \"\"\"x" + strings.Repeat("\"", cancelCheckInterval*4)),
		ctx:    &cancelOnErrCallContext{cancelAt: 1},
		limits: testLimits(64, 1<<20, 1<<20, 1<<20, 1<<20),
	}
	if err := scan.document(); !errors.Is(err, context.Canceled) {
		t.Fatalf("document() = %v, want context.Canceled", err)
	}
}

func TestScannerChecksCancellationBeforeSuccessfulReturn(t *testing.T) {
	ctx := &cancelOnErrCallContext{cancelAt: 1}
	scan := scanner{
		src:    []byte("k = 1\n"),
		ctx:    ctx,
		limits: StandardLimits(),
	}
	if err := scan.document(); !errors.Is(err, context.Canceled) {
		t.Fatalf("document() = %v, want final context.Canceled", err)
	}
}

func TestDecodeAdmittedChecksCancellationBeforeAndAfterSuccessfulDecode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var preDecode map[string]any
	if _, err := DecodeAdmitted(ctx, []byte("value = 1\n"), &preDecode); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeAdmitted(pre-canceled) = %v, want context.Canceled", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	postDecode := struct {
		Value cancelingTOMLValue `toml:"value"`
	}{Value: cancelingTOMLValue{cancel: cancel}}
	if _, err := DecodeAdmitted(ctx, []byte("value = 1\n"), &postDecode); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeAdmitted(post-canceled) = %v, want context.Canceled", err)
	}
}

func TestDecodeAdmittedPreservesDecoderErrorOverConcurrentCancellation(t *testing.T) {
	decodeErr := errors.New("decode failed")
	ctx, cancel := context.WithCancel(context.Background())
	decoded := struct {
		Value cancelingTOMLValue `toml:"value"`
	}{Value: cancelingTOMLValue{cancel: cancel, err: decodeErr}}
	_, err := DecodeAdmitted(ctx, []byte("value = 1\n"), &decoded)
	if err == nil || !strings.Contains(err.Error(), decodeErr.Error()) || errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeAdmitted(decoder error) = %v, want decoder failure rather than cancellation", err)
	}
}

func TestAdmitRejectsNonPositiveLimits(t *testing.T) {
	err := Admit(context.Background(), []byte("k = 1\n"), Limits{})
	if err == nil {
		t.Fatal("Admit(zero limits) = nil, want error")
	}
}

func TestAdmitWithUsageIgnoresTriviaAndPrimitivePayloadLength(t *testing.T) {
	compact, err := AdmitWithUsage(
		context.Background(),
		[]byte(`root={child="x"}`),
		StandardLimits(),
	)
	if err != nil {
		t.Fatalf("AdmitWithUsage(compact): %v", err)
	}
	padded, err := AdmitWithUsage(
		context.Background(),
		[]byte("  # leading comment\nroot = { child = \""+strings.Repeat("payload ", 1024)+"\" } # trailing comment\n"),
		StandardLimits(),
	)
	if err != nil {
		t.Fatalf("AdmitWithUsage(padded): %v", err)
	}
	if compact != padded {
		t.Fatalf("structure usage changed with trivia/payload: compact=%#v padded=%#v", compact, padded)
	}
	if got, want := compact.StructuralUnits(), compact.keyBytes+compact.containers; got != want {
		t.Fatalf("StructuralUnits = %d, want %d", got, want)
	}
}

func TestStructureUsageValidatesExactLimitsAndClassifiesOverflow(t *testing.T) {
	usage := StructureUsage{containers: 3, work: 5, pathWork: 7, keyBytes: 11}
	exact := testLimits(8, 3, 5, 64, 7)
	if err := usage.Validate(exact); err != nil {
		t.Fatalf("Validate(exact): %v", err)
	}

	tests := []struct {
		name   string
		limits Limits
		want   error
	}{
		{name: "containers", limits: testLimits(8, 2, 5, 64, 7), want: ErrMaximumContainersExceeded},
		{name: "work", limits: testLimits(8, 3, 4, 64, 7), want: ErrMaximumWorkExceeded},
		{name: "path work", limits: testLimits(8, 3, 5, 64, 6), want: ErrMaximumPathWorkExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := usage.Validate(test.limits); !errors.Is(err, test.want) {
				t.Fatalf("Validate = %v, want %v", err, test.want)
			}
		})
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

func TestAdmitAcceptsExactPathWorkOfLongAncestorSiblings(t *testing.T) {
	if MaximumPathWork%MaximumKeyBytes != 0 {
		t.Fatalf("MaximumPathWork=%d is not a multiple of MaximumKeyBytes=%d", MaximumPathWork, MaximumKeyBytes)
	}
	siblings := MaximumPathWork / MaximumKeyBytes
	content := longAncestorSiblings(MaximumKeyBytes, siblings)
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(pathWork=%d) = %v, want nil", MaximumPathWork, err)
	}
}

func TestAdmitRejectsLongAncestorSiblingsBeyondPathWork(t *testing.T) {
	siblings := MaximumPathWork/MaximumKeyBytes + 1
	content := longAncestorSiblings(MaximumKeyBytes, siblings)
	started := time.Now()
	err := Admit(context.Background(), []byte(content), StandardLimits())
	elapsed := time.Since(started)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(pathWork=%d) = %v, want path recopy exceeded", MaximumPathWork+MaximumKeyBytes, err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Admit(long ancestor siblings) took %s, want fail-closed before Unmarshal cost", elapsed)
	}
}

func TestAdmitAcceptsExactParameterizedPathWork(t *testing.T) {
	limits := testLimits(8, 8, 64, 64, 32)
	content := longAncestorSiblings(8, 4)
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(pathWork=32) = %v, want nil", err)
	}
}

func TestAdmitRejectsParameterizedPathWorkPlusOne(t *testing.T) {
	limits := testLimits(8, 8, 64, 64, 32)
	err := Admit(context.Background(), []byte(longAncestorSiblings(8, 5)), limits)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(pathWork=40) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitChargesDottedAssignmentAgainstAncestorPathBytes(t *testing.T) {
	limits := testLimits(8, 8, 64, 64, 15)
	content := "[aaaaaaaa]\na.b = 1\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(dotted recopy 16>15) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitRejectsNestedLongKeySiblingRecopy(t *testing.T) {
	siblings := MaximumPathWork/MaximumKeyBytes + 1
	content := "k = { " + strings.Repeat("a", MaximumKeyBytes) + " = { " +
		strings.Repeat("x = 1, ", siblings-1) + "x = 1 } }\n"
	err := Admit(context.Background(), []byte(content), StandardLimits())
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(nested long-key siblings) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitAcceptsRootLevelLongKeyWithoutRecopy(t *testing.T) {
	content := strings.Repeat("a", MaximumKeyBytes) + " = 1\n"
	if err := Admit(context.Background(), []byte(content), StandardLimits()); err != nil {
		t.Fatalf("Admit(root long key) = %v, want nil", err)
	}
}

func TestAdmitAcceptsPluginTableCountUnderObservationBudget(t *testing.T) {
	const pluginTables = 4096
	var builder strings.Builder
	builder.Grow(pluginTables * 40)
	for index := range pluginTables {
		builder.WriteString("[plugins.\"p")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString("\"]\nenabled = true\n")
	}
	if err := Admit(context.Background(), []byte(builder.String()), StandardLimits()); err != nil {
		t.Fatalf("Admit(%d plugin tables) = %v, want nil so observation budget remains first", pluginTables, err)
	}
}

func TestAdmitPathWorkMultiplyOverflowFailsClosed(t *testing.T) {
	scanner := scanner{
		ctx:       context.Background(),
		limits:    StandardLimits(),
		pathBytes: math.MaxInt,
	}
	err := scanner.chargeKey(2, 1)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("chargeKey(MaxInt pathBytes, 2 parts) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitAcceptsExactAnonymousNestedArrayPathWork(t *testing.T) {
	limits := testLimits(8, 16, 64, 64, 32)
	content := "aaaaaaaa = [[], [], [], []]\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(anonymous nested array pathWork=32) = %v, want nil", err)
	}
}

func TestAdmitRejectsAnonymousNestedArrayPathWorkPlusOne(t *testing.T) {
	limits := testLimits(8, 16, 64, 64, 32)
	content := "aaaaaaaa = [[], [], [], [], []]\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(anonymous nested array pathWork=40) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitAcceptsExactAnonymousInlineTablePathWork(t *testing.T) {
	limits := testLimits(8, 16, 64, 64, 32)
	content := "aaaaaaaa = [{}, {}, {}, {}]\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(anonymous inline table pathWork=32) = %v, want nil", err)
	}
}

func TestAdmitRejectsAnonymousInlineTablePathWorkPlusOne(t *testing.T) {
	limits := testLimits(8, 16, 64, 64, 32)
	content := "aaaaaaaa = [{}, {}, {}, {}, {}]\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(anonymous inline table pathWork=40) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitDoesNotChargePrimitiveArrayElementsAsPathWork(t *testing.T) {
	limits := testLimits(8, 16, 64, 64, 1)
	content := "aaaaaaaa = [1, 2, 3, 4, 5]\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(primitive array elements) = %v, want nil", err)
	}
}

func TestAdmitRejectsLongAncestorAnonymousArraysBeyondPathWork(t *testing.T) {
	elements := MaximumPathWork/MaximumKeyBytes + 1
	content := longKeyArrayElements(MaximumKeyBytes, elements, "[]")
	started := time.Now()
	err := Admit(context.Background(), []byte(content), StandardLimits())
	elapsed := time.Since(started)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(anonymous array pathWork=%d) = %v, want path recopy exceeded", MaximumPathWork+MaximumKeyBytes, err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Admit(long ancestor anonymous arrays) took %s, want fail-closed before Unmarshal cost", elapsed)
	}
}

func TestAdmitChargesArrayInlineTableSiblingsAgainstArrayKeyBytes(t *testing.T) {
	limits := testLimits(8, 16, 64, 64, 32)
	content := "aaaaaaaa = [{k = 1}, {k = 1}, {k = 1}, {k = 1}, {k = 1}]\n"
	err := Admit(context.Background(), []byte(content), limits)
	if !errors.Is(err, ErrMaximumPathWorkExceeded) {
		t.Fatalf("Admit(array inline siblings) = %v, want path recopy exceeded", err)
	}
}

func TestAdmitResetsPathBytesOnTableHeaderSwitch(t *testing.T) {
	limits := testLimits(8, 8, 64, 64, 8)
	content := "[aaaaaaaa]\n[b]\nk = 1\n"
	if err := Admit(context.Background(), []byte(content), limits); err != nil {
		t.Fatalf("Admit(header switch) = %v, want nil after path reset", err)
	}
}

type cancelingTOMLValue struct {
	cancel context.CancelFunc
	err    error
}

func (value *cancelingTOMLValue) UnmarshalTOML(any) error {
	value.cancel()
	return value.err
}

type cancelOnErrCallContext struct {
	calls    int
	cancelAt int
}

func (ctx *cancelOnErrCallContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *cancelOnErrCallContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelOnErrCallContext) Value(any) any               { return nil }
func (ctx *cancelOnErrCallContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

type cancelAtScannerIndexContext struct {
	scan       *scanner
	threshold  int
	canceledAt int
}

func (ctx *cancelAtScannerIndexContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *cancelAtScannerIndexContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelAtScannerIndexContext) Value(any) any               { return nil }
func (ctx *cancelAtScannerIndexContext) Err() error {
	if ctx.scan.index < ctx.threshold {
		return nil
	}
	if ctx.canceledAt == 0 {
		ctx.canceledAt = ctx.scan.index
	}
	return context.Canceled
}

func testLimits(depth int, containers int, work int, keyBytes int, pathWork int) Limits {
	return Limits{
		MaximumDepth:      depth,
		MaximumContainers: containers,
		MaximumWork:       work,
		MaximumKeyBytes:   keyBytes,
		MaximumPathWork:   pathWork,
	}
}

func longAncestorSiblings(keyBytes int, siblings int) string {
	var builder strings.Builder
	builder.Grow(keyBytes + siblings*7 + 4)
	builder.WriteByte('[')
	builder.WriteString(strings.Repeat("a", keyBytes))
	builder.WriteString("]\n")
	for range siblings {
		builder.WriteString("k = 1\n")
	}
	return builder.String()
}

func longKeyArrayElements(keyBytes int, elements int, value string) string {
	var builder strings.Builder
	builder.Grow(keyBytes + elements*(len(value)+2) + 6)
	builder.WriteString(strings.Repeat("a", keyBytes))
	builder.WriteString(" = [")
	for index := range elements {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(value)
	}
	builder.WriteString("]\n")
	return builder.String()
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
