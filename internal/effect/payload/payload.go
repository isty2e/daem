package payload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/topology"
)

// FilePayload is immutable effect-ready regular-file content.
type FilePayload struct {
	content []byte
	hash    artifact.ContentHash
	mode    fs.FileMode
}

// Bytes returns an independent copy of the effect-ready file bytes.
func (payload FilePayload) Bytes() []byte { return append([]byte(nil), payload.content...) }

// Hash returns the content identity including executable class.
func (payload FilePayload) Hash() artifact.ContentHash { return payload.hash }

// Mode returns the exact permission mode requested by the locked realization.
func (payload FilePayload) Mode() fs.FileMode { return payload.mode }

func (payload FilePayload) validate() error {
	if payload.mode != payload.mode.Perm() {
		return fmt.Errorf("file payload mode %04o contains non-permission bits", payload.mode)
	}
	if err := payload.hash.Validate(); err != nil {
		return fmt.Errorf("file payload content hash: %w", err)
	}
	actual := artifact.HashFileContentWithExecutable(
		payload.content,
		payload.mode.Perm()&0o111 != 0,
	)
	if payload.hash != actual {
		return fmt.Errorf("file payload hash does not match content and executable class")
	}
	return nil
}

// DirectoryPayload is an exact directory identity plus the non-owning view
// needed to copy it into unpublished Effect staging.
type DirectoryPayload struct {
	identity artifact.ExactIdentity
	view     access.View
}

// Identity returns the exact directory artifact identity.
func (payload DirectoryPayload) Identity() artifact.ExactIdentity { return payload.identity }

// View returns the non-owning directory access capability.
func (payload DirectoryPayload) View() access.View { return payload.view }

// Hash returns the exact directory content identity.
func (payload DirectoryPayload) Hash() artifact.ContentHash {
	return payload.identity.ContentHash()
}

func (payload DirectoryPayload) validate() error {
	if err := payload.identity.Validate(); err != nil {
		return fmt.Errorf("directory payload identity: %w", err)
	}
	if payload.identity.Kind() != artifact.ArtifactKindDirectory ||
		payload.view.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("directory payload requires directory identity and access")
	}
	return nil
}

// Payload is a closed effect-ready value with exactly one private content variant.
type Payload struct {
	subject   topology.SubjectID
	file      *FilePayload
	directory *DirectoryPayload
}

// NewFilePayload constructs immutable file content and derives its exact hash.
func NewFilePayload(subject topology.SubjectID, content []byte, mode fs.FileMode) (Payload, error) {
	file := FilePayload{
		content: append([]byte(nil), content...),
		mode:    mode.Perm(),
	}
	file.hash = artifact.HashFileContentWithExecutable(
		file.content,
		file.mode&0o111 != 0,
	)
	payload := Payload{subject: subject, file: &file}
	if err := payload.validate(); err != nil {
		return Payload{}, err
	}
	return payload, nil
}

// NewDirectoryPayload constructs exact directory content for an Effect copy.
func NewDirectoryPayload(
	ctx context.Context,
	subject topology.SubjectID,
	identity artifact.ExactIdentity,
	view access.View,
) (Payload, error) {
	directory := DirectoryPayload{identity: identity, view: view}
	payload := Payload{subject: subject, directory: &directory}
	if err := payload.validate(); err != nil {
		return Payload{}, err
	}
	if err := view.Verify(ctx, identity); err != nil {
		return Payload{}, fmt.Errorf("directory payload access: %w", err)
	}
	return payload, nil
}

// Subject returns the canonical locked subject this payload realizes.
func (payload Payload) Subject() topology.SubjectID { return payload.subject }

// Hash returns the exact effect-ready content identity.
func (payload Payload) Hash() artifact.ContentHash {
	if payload.file != nil && payload.directory == nil {
		return payload.file.Hash()
	}
	if payload.file == nil && payload.directory != nil {
		return payload.directory.Hash()
	}
	return ""
}

// File returns the file variant when present.
func (payload Payload) File() (FilePayload, bool) {
	if payload.file == nil || payload.directory != nil {
		return FilePayload{}, false
	}
	return *payload.file, true
}

// Directory returns the directory variant when present.
func (payload Payload) Directory() (DirectoryPayload, bool) {
	if payload.directory == nil || payload.file != nil {
		return DirectoryPayload{}, false
	}
	return *payload.directory, true
}

// PayloadSet owns materialized payloads and any temporary resources backing them.
type PayloadSet struct {
	bySubject map[topology.SubjectID]Payload
	cleanup   *payloadCleanup
}

type payloadCleanup struct {
	once  sync.Once
	funcs []func() error
	err   error
}

// NewPayloadSet owns materialized payloads and cleanup callbacks.
func NewPayloadSet(values []Payload, cleanups []func() error) (PayloadSet, error) {
	bySubject := make(map[topology.SubjectID]Payload, len(values))
	for _, value := range values {
		if err := value.validate(); err != nil {
			return PayloadSet{}, err
		}
		if _, exists := bySubject[value.Subject()]; exists {
			return PayloadSet{}, fmt.Errorf(
				"duplicate payload for subject %s/%s %q",
				value.Subject().Kind(),
				value.Subject().Namespace(),
				value.Subject().Key(),
			)
		}
		bySubject[value.Subject()] = value
	}

	return PayloadSet{
		bySubject: bySubject,
		cleanup:   &payloadCleanup{funcs: append([]func() error(nil), cleanups...)},
	}, nil
}

// LookupSubject returns the payload for a locked subject identity.
func (set PayloadSet) LookupSubject(subject topology.SubjectID) (Payload, bool) {
	payload, ok := set.bySubject[subject]
	return payload, ok
}

// VerifyHash rejects payloads that do not match the planned desired hash.
func (payload Payload) VerifyHash(expected artifact.ContentHash, destination output.Destination) error {
	if err := payload.validate(); err != nil {
		return fmt.Errorf("host payload for %q: %w", destination, err)
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("planned host payload hash for %q: %w", destination, err)
	}
	actual := payload.Hash()
	if actual != expected {
		return fmt.Errorf("host payload hash %q does not match planned hash %q for %q", actual, expected, destination)
	}

	return nil
}

// Cleanup releases temporary resources backing materialized payloads and
// returns the stable joined cleanup result on every call.
func (set PayloadSet) Cleanup() error {
	if set.cleanup != nil {
		return set.cleanup.run()
	}
	return nil
}

func (cleanup *payloadCleanup) run() error {
	cleanup.once.Do(func() {
		var cleanupErrors []error
		for index := len(cleanup.funcs) - 1; index >= 0; index-- {
			if cleanup.funcs[index] == nil {
				continue
			}
			cleanupErrors = append(cleanupErrors, cleanup.funcs[index]())
		}
		cleanup.err = errors.Join(cleanupErrors...)
	})
	return cleanup.err
}

func (payload Payload) validate() error {
	if err := payload.subject.Validate(); err != nil {
		return fmt.Errorf("payload subject: %w", err)
	}
	switch {
	case payload.file != nil && payload.directory == nil:
		return payload.file.validate()
	case payload.file == nil && payload.directory != nil:
		return payload.directory.validate()
	default:
		return fmt.Errorf("payload requires exactly one file or directory content variant")
	}
}
