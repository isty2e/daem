package configdocument_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func TestLeavesBoundsTotalEncodedPaths(t *testing.T) {
	prefix := strings.Repeat("long/~", 128)
	children := make(map[string]bool)
	want := make(map[output.KeyPath]struct{})
	var maximum int64
	for index := range 128 {
		key := fmt.Sprintf("child%d", index)
		children[key] = true
		selected, err := output.NewKeyPath(prefix, key)
		if err != nil {
			t.Fatal(err)
		}
		want[selected] = struct{}{}
		maximum += int64(len(selected.String()))
	}
	content, err := json.Marshal(map[string]map[string]bool{prefix: children})
	if err != nil {
		t.Fatal(err)
	}
	document := parse(t, configdocument.JSON, string(content))
	entries, err := document.Leaves(t.Context(), maximum)
	if err != nil || len(entries) != len(want) {
		t.Fatalf("exact path allowance refused: count=%d, err=%v", len(entries), err)
	}
	for _, entry := range entries {
		if _, found := want[entry.Path]; !found {
			t.Fatalf("unexpected or repeated leaf %q", entry.Path)
		}
		delete(want, entry.Path)
		assertEqual(t, entry.Value, lookup(t, document, entry.Path.String()))
	}
	for _, maximum := range []int64{0, int64(len(content)), maximum - 1} {
		entries, err := document.Leaves(t.Context(), maximum)
		if !errors.Is(err, configdocument.ErrLimitExceeded) || entries != nil {
			t.Fatal("over-budget projection returned data or lost its failure cause")
		}
	}
}

func TestLeavesCancellationAndEmptyProjection(t *testing.T) {
	document := parse(t, configdocument.JSON, `{"v":true}`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	entries, err := document.Leaves(ctx, testLimits.MaximumBytes)
	if !errors.Is(err, context.Canceled) || entries != nil {
		t.Fatal("canceled projection returned entries")
	}
	for _, fixture := range []struct {
		ctx     context.Context
		maximum int64
	}{{nil, 1}, {t.Context(), -1}} {
		entries, err := document.Leaves(fixture.ctx, fixture.maximum)
		if err == nil || entries != nil {
			t.Fatal("invalid projection policy was admitted")
		}
	}
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		absent, err := configdocument.Absent(syntax, testLimits)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := absent.Leaves(t.Context(), 0)
		if err != nil || len(entries) != 0 {
			t.Fatal("absent document consumed path allowance")
		}
	}
}
