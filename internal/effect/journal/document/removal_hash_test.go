package document

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestNativeRemovalUsesExecutableHostHashNotArtifactDigest(t *testing.T) {
	document := aggregate.ExistingDocument([]byte("#!/bin/sh\nexit 0\n"))
	mode := recovery.NewPermissionMode(0o700)
	states, err := RemovalStates(document, mode, aggregate.AbsentDocument(), nil)
	if err != nil || len(states) != 1 {
		t.Fatalf("executable before-state lost: %v", err)
	}
	before, present := states[0].Before()
	if !present || before.ContentHash != string(artifact.HashFileContentWithExecutable(document.Content(), true)) || before.ContentHash == string(artifact.HashFileContent(document.Content())) {
		t.Fatal("removal basis substituted the private non-executable artifact hash")
	}
}
