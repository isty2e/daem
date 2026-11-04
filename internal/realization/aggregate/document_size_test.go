package aggregate_test

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestDocumentContentSizeCountsOwnedBytesIndependentlyOfPresence(t *testing.T) {
	for _, document := range []aggregate.Document{aggregate.AbsentDocument(), aggregate.ExistingDocument(nil)} {
		if document.ContentSize() != 0 {
			t.Fatal("absent/empty document has content bytes")
		}
	}
	document := aggregate.ExistingDocument([]byte("한글"))
	if document.ContentSize() != int64(len([]byte("한글"))) {
		t.Fatal("document size does not describe its immutable UTF-8 payload bytes")
	}
}
