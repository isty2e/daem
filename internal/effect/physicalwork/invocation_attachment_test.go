package physicalwork

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestInvocationAttachmentIsPrepaidAndSeparateFromCommandAndSafety(t *testing.T) {
	shared := &invocationPathEnvelope{limit: 100, cause: errors.New("shared limit")}
	parent := NewCaptureBudget(shared)
	attachment, err := NewInvocationWork([]int{2}, artifactWork(t, 1, 3))
	if err != nil {
		t.Fatal(err)
	}
	forward, err := NewInvocationWork([]int{4}, artifactWork(t, 2, 5))
	if err != nil {
		t.Fatal(err)
	}
	safety, err := NewInvocationWork([]int{6}, artifactWork(t, 3, 7))
	if err != nil {
		t.Fatal(err)
	}
	first, err := parent.ReserveInvocationWithAttachment(attachment, forward, safety)
	if err != nil {
		t.Fatal(err)
	}
	second, err := parent.ReserveInvocationWithAttachment(attachment, forward, safety)
	if err != nil || shared.used != 24 || parent.entries != 12 || parent.bytes != 30 {
		t.Fatal("all phases were not charged before transfer", err)
	}
	if command, cleanup, err := first.Take(); err == nil || command != nil || cleanup != nil {
		t.Fatal("command credit transferred before attachment")
	}
	copy := first
	observing, err := first.TakeAttachment()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copy.TakeAttachment(); err == nil {
		t.Fatal("a copied handle duplicated attachment credit")
	}
	if _, err := parent.ReserveInvocation(InvocationWork{}, InvocationWork{}); err == nil {
		t.Fatal("attachment transfer left new command admission open")
	}
	if _, err := parent.ReserveInvocationWithAttachment(attachment, forward, safety); err == nil {
		t.Fatal("attachment transfer left new attachment admission open")
	}
	if err := observing.AdmitPhysicalWork(2, 1, 3); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []struct {
		paths, entries int
		bytes          int64
	}{{paths: 1}, {entries: 1}, {bytes: 1}} {
		if err := observing.AdmitPhysicalWork(extra.paths, extra.entries, extra.bytes); err == nil {
			t.Fatal("attachment consumed command or safety credit")
		}
	}
	command, cleanup, err := first.Take()
	if err != nil {
		t.Fatal("attachment exhaustion consumed later phases", err)
	}
	if err := command.AdmitPhysicalWork(4, 2, 5); err != nil {
		t.Fatal(err)
	}
	if err := cleanup.AdmitPhysicalWork(6, 3, 7); err != nil {
		t.Fatal(err)
	}
	if _, _, err := copy.Take(); err == nil {
		t.Fatal("a copied handle duplicated command/safety credit")
	}
	if _, err := second.TakeAttachment(); err != nil {
		t.Fatal("a separately prepaid peer lost its attachment", err)
	}
	if _, _, err := second.Take(); err != nil {
		t.Fatal("a separately prepaid peer lost its command", err)
	}
	if shared.used != 24 {
		t.Fatal("phase consumption charged the parent again")
	}
}

func TestInvocationAttachmentReservationRefusesCompleteOverdraw(t *testing.T) {
	for _, dimension := range []string{"path", "entries", "bytes"} {
		t.Run(dimension, func(t *testing.T) {
			shared := &invocationPathEnvelope{limit: 100, cause: errors.New("shared limit")}
			parent := NewCaptureBudget(shared)
			switch dimension {
			case "path":
				parent.pathComponentLimit = 5
			case "entries":
				parent.entryLimit = 5
			case "bytes":
				parent.byteLimit = 5
			}
			work, err := NewInvocationWork([]int{2}, artifactWork(t, 2, 2))
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := parent.ReserveInvocationWithAttachment(work, work, work)
			if err == nil || shared.used != 0 || parent.entries != 0 || parent.bytes != 0 || parent.pathComponents != 0 {
				t.Fatal("three-phase capacity refusal partially admitted work", err)
			}
			if _, err := reservation.TakeAttachment(); err == nil {
				t.Fatal("failed reservation returned attachment credit")
			}
			if _, _, err := reservation.Take(); err == nil {
				t.Fatal("failed reservation returned command credit")
			}
		})
	}
}

func TestInvocationAttachmentFailureRetainsSpentSharedWork(t *testing.T) {
	cause := errors.New("shared limit")
	shared := &invocationPathEnvelope{limit: 3, cause: cause}
	parent := NewCaptureBudget(shared)
	work, err := NewInvocationWork([]int{2}, workmeasure.ArtifactWork{})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := parent.ReserveInvocationWithAttachment(work, work, InvocationWork{})
	if !errors.Is(err, cause) || shared.used != 2 || parent.pathComponents != 2 {
		t.Fatal("shared refusal reset an already charged attachment", err)
	}
	if _, err := reservation.TakeAttachment(); err == nil {
		t.Fatal("failed shared admission returned credit")
	}
	if _, err := parent.ReserveInvocationWithAttachment(work, InvocationWork{}, InvocationWork{}); !errors.Is(err, cause) {
		t.Fatal("retry bypassed spent parent work", err)
	}
}

func TestZeroWorkAttachmentStillHasOneTransfer(t *testing.T) {
	parent := NewCaptureBudget(nil)
	prospective, err := parent.ReserveInvocationWithAttachment(InvocationWork{}, InvocationWork{}, InvocationWork{})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := parent.ReserveInvocation(InvocationWork{}, InvocationWork{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := prospective.Take(); err == nil {
		t.Fatal("zero attachment erased its required phase")
	}
	if _, err := prospective.TakeAttachment(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prospective.Take(); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.TakeAttachment(); err == nil {
		t.Fatal("legacy reservation gained an attachment phase")
	}
	if _, _, err := legacy.Take(); err != nil {
		t.Fatal("legacy transfer changed", err)
	}
	if _, err := (InvocationWorkReservation{}).TakeAttachment(); err == nil {
		t.Fatal("zero reservation gained attachment credit")
	}
}
