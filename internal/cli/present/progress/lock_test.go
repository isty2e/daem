package progress_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	cliprogress "github.com/isty2e/daem/internal/cli/present/progress"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestLockProgressRendererShowsCorrelatedEphemeralProgress(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: &output})
	renderer.LockSink()(cliprogress.LockEvent{Kind: "resource_resolve_started", TaskID: "skill:0", EntityID: progressEntityID(t, "oracle")})
	renderer.SourceSink()(newSourceProgressEvent(t, acquisition.EventStarted, "skill:0"))
	renderer.LockSink()(cliprogress.LockEvent{Kind: "resource_locked", TaskID: "skill:0", EntityID: progressEntityID(t, "oracle")})
	renderer.Close()

	got := output.String()
	for _, want := range []string{"\r\x1b[2KResolving sources 0: skill/oracle", "\r\x1b[2KResolving sources 1: skill/oracle", "\r\x1b[2K"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	for _, forbidden := range []string{"request=", "ordinal=", "stage=", "source_kind=", "resolved_ref="} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("output = %q, did not want %q", got, forbidden)
		}
	}
}

func newSourceProgressEvent(t *testing.T, kind acquisition.EventKind, requestID acquisition.RequestID) acquisition.Event {
	t.Helper()
	sourceSpec := sourcetest.Local(t, "skills/oracle", source.LocalSourceModeVendor)
	request, err := acquisition.NewRequest(requestID, 0, acquisition.OperationResolve, sourceSpec)
	if err != nil {
		t.Fatalf("new source request: %v", err)
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("source id: %v", err)
	}
	event, err := acquisition.NewEvent(kind, request, sourceID, "", nil)
	if err != nil {
		t.Fatalf("new source event: %v", err)
	}
	return event
}

func TestLockProgressRendererDeduplicatesEquivalentUpdates(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: &output})
	event := cliprogress.LockEvent{Kind: "resource_resolve_started", TaskID: "skill:0", EntityID: progressEntityID(t, "oracle")}
	renderer.LockSink()(event)
	renderer.LockSink()(event)
	if got := strings.Count(output.String(), "Resolving sources"); got != 1 {
		t.Fatalf("updates = %d, want 1; output=%q", got, output.String())
	}
}

func TestLockProgressRendererDoesNotDoubleCountRepeatedCompletion(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: &output})
	event := cliprogress.LockEvent{Kind: "resource_locked", TaskID: "skill:0", EntityID: progressEntityID(t, "oracle")}
	renderer.LockSink()(event)
	renderer.LockSink()(event)
	if strings.Contains(output.String(), "Resolving sources 2") || strings.Count(output.String(), "Resolving sources 1") != 1 {
		t.Fatalf("output = %q, want one idempotent completion", output.String())
	}
}

func TestLockProgressRendererEscapesLabelsAndHidesErrors(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: &output})
	renderer.LockSink()(cliprogress.LockEvent{Kind: "resource_resolve_failed", TaskID: "skill:0", EntityID: progressEntityID(t, "bad\n\x1b[31m"), Err: errors.New("private detail")})
	got := output.String()
	if strings.Contains(got, "bad\n") || strings.Contains(got, "\x1b[31m") || strings.Contains(got, "private detail") {
		t.Fatalf("output leaked control/error text: %q", got)
	}
	if !strings.Contains(got, `bad\n\x1b[31m`) || !strings.Contains(got, ": failed") {
		t.Fatalf("output = %q, want escaped label and failed state", got)
	}
}

func TestLockProgressRendererSuppressesAfterWriteError(t *testing.T) {
	writer := &failAfterFirstWrite{}
	renderer := cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: writer})
	renderer.LockSink()(cliprogress.LockEvent{Kind: "resource_resolve_started", EntityID: progressEntityID(t, "one")})
	renderer.LockSink()(cliprogress.LockEvent{Kind: "resource_locked", EntityID: progressEntityID(t, "one")})
	renderer.Close()
	if writer.writeAttempts != 2 {
		t.Fatalf("write attempts = %d, want 2", writer.writeAttempts)
	}
}

func TestLockProgressRendererAcceptsConcurrentEvents(t *testing.T) {
	var output bytes.Buffer
	renderer := cliprogress.NewLockProgressRenderer(cliprogress.LockProgressRendererOptions{Output: &output})
	var waitGroup sync.WaitGroup
	for index := range 32 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			renderer.LockSink()(cliprogress.LockEvent{Kind: "resource_resolve_started", TaskID: acquisition.RequestID(fmt.Sprintf("skill:%d", index)), EntityID: progressEntityID(t, fmt.Sprintf("skill-%d", index))})
		}(index)
	}
	waitGroup.Wait()
	if !strings.Contains(output.String(), "Resolving sources") {
		t.Fatalf("output = %q, want progress", output.String())
	}
}

func progressEntityID(t *testing.T, name string) entity.ID {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	return id
}

func TestLockProgressRendererNilReceiverSinksAreNoop(t *testing.T) {
	var renderer *cliprogress.LockProgressRenderer
	if renderer.LockSink() != nil || renderer.SourceSink() != nil {
		t.Fatalf("nil renderer returned non-nil sink")
	}
	renderer.Close()
}

type failAfterFirstWrite struct {
	buffer        bytes.Buffer
	writeAttempts int
}

func (writer *failAfterFirstWrite) Write(content []byte) (int, error) {
	writer.writeAttempts++
	if writer.writeAttempts > 1 {
		return 0, errors.New("stderr closed")
	}
	return writer.buffer.Write(content)
}
