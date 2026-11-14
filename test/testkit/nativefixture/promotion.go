package nativefixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func (store *nativeRecoveryStore) ReplaceRootedFileAndRefreshParent(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode, expected, expectedParent mutationfs.EntryIdentity) (mutationfs.CommitOutcome, mutationfs.EntryIdentity, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, nil, err
	}
	outcome, parent, err := store.Adapter.ReplaceRootedFileAndRefreshParent(ctx, capability, content, mode, expected, expectedParent)
	ordinary := store.boundary == "ordinary-promotion" || store.boundary == "return-ordinary-promotion-error"
	if err != nil || filepath.Base(path) != "journal.json" || !ordinary && store.boundary != "promotion" && store.boundary != "return-promotion-error" {
		return outcome, parent, err
	}

	actual, err := os.ReadFile(path)
	if err != nil {
		return outcome, parent, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return outcome, parent, err
	}
	if !bytes.Equal(actual, content) || info.Mode().Perm() != mode.Perm() || outcome.State() != mutationfs.CommitOutcomeComplete || parent == nil {
		return outcome, parent, errors.New("native recovery fixture: group promotion was not confirmed")
	}
	if ordinary {
		var record struct {
			Pending []json.RawMessage `json:"provisional_acquires"`
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(actual, &record); err != nil {
			return outcome, parent, err
		}
		if len(record.Pending) != 0 || len(record.Entries) == 0 {
			return outcome, parent, nil
		}
		fmt.Fprintln(os.Stderr, "native recovery confirmed ordinary promotion")
		if store.boundary == "ordinary-promotion" {
			os.Exit(RecoveryExit)
		}
		return outcome, parent, errors.New("native recovery fixture: returned ordinary promotion error")
	}
	fmt.Fprintln(os.Stderr, "native recovery confirmed group promotion")
	if store.boundary == "promotion" {
		os.Exit(RecoveryExit)
	}
	if !store.failed {
		store.failed = true
		return outcome, parent, errors.New("native recovery fixture: returned promotion error")
	}
	return outcome, parent, nil
}
