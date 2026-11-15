package configdocument

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

type cancelOnSecondLayoutCheck struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (ctx *cancelOnSecondLayoutCheck) Err() error {
	if ctx.checks.Add(1) == 2 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestJSONLayoutEvidenceCancelsAcrossCommentSkips(t *testing.T) {
	comments := strings.Repeat("/**/", 8192)
	content := []byte("{" + comments + `"a":1}`)
	trivia, err := scanJSON(t.Context(), content, JSONC, 8)
	if err != nil {
		t.Fatal(err)
	}
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &cancelOnSecondLayoutCheck{Context: base, cancel: cancel}
	source := jsonLayoutSource{ctx: ctx, content: content, comments: trivia.spans}
	_, err = source.evidence(1, 1+len(comments))
	if !errors.Is(err, context.Canceled) {
		t.Fatal("comment-span traversal skipped cancellation", err)
	}
}
