package ownership

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

// Fingerprint binds scoped semantic facts, never the shared store's revision.
func (view DocumentRegistryView) Fingerprint() (string, error) {
	if _, err := NewDocumentScope(view.scope.exact, view.scope.missing); err != nil || !view.scope.initialized {
		return "", fmt.Errorf("ownership fingerprint requires an initialized document view")
	}
	w := registryIdentityWriter{sha256.New()}
	w.strings("ownership-document-view-v1")
	w.count(len(view.scope.exact))
	for _, namespace := range view.scope.exact {
		w.path(namespace)
	}
	w.count(len(view.scope.missing))
	for _, namespace := range view.scope.missing {
		w.strings(namespace.CandidateKey(), namespace.CandidateWitness())
		w.path(namespace.Namespace())
	}
	w.count(len(view.claims))
	for _, claim := range view.claims {
		w.path(claim.Address().PathAuthority())
		footprint := claim.Address().Footprint()
		w.strings(footprint.Kind().String())
		if key, present := footprint.Key(); present {
			w.strings(key.String())
		}
		w.owner(claim.Owner())
		w.strings(string(claim.State()), claim.OperationID())
	}
	w.bindings(view.bindings)
	w.count(len(view.protections))
	for _, protection := range view.protections {
		w.owner(protection.Owner())
		correlation := protection.Correlation()
		w.strings(protection.Invocation().String(), correlation.OperationID(), correlation.RequestFingerprint(), correlation.SelectionFingerprint(), string(protection.Phase()))
		w.count(len(protection.documents))
		for _, document := range protection.documents {
			w.bindings(document.bindings)
			w.strings(strconv.FormatUint(uint64(document.userID), 10), strconv.FormatUint(uint64(document.groupID), 10), strconv.FormatUint(uint64(document.modeCap), 10))
		}
		if execution, bound := protection.Execution(); bound {
			context := execution.Context()
			w.strings(string(context.Platform()), context.Realm(), context.Epoch(), strconv.FormatInt(execution.Group(), 10), execution.Invocation().String())
		}
	}
	return hex.EncodeToString(w.Sum(nil)), nil
}

type registryIdentityWriter struct{ hash.Hash }

func (writer registryIdentityWriter) strings(values ...string) {
	for _, value := range values {
		writer.count(len(value))
		_, _ = writer.Write([]byte(value))
	}
}

func (writer registryIdentityWriter) count(value int) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(value))
	_, _ = writer.Write(size[:])
}

func (writer registryIdentityWriter) path(value pathauthority.Exact) {
	writer.strings(value.Key(), value.Witness())
}

func (writer registryIdentityWriter) owner(value stateauthority.Authority) {
	writer.path(value.StatefileAuthority())
	writer.strings(value.ManifestPath())
}

func (writer registryIdentityWriter) bindings(values []DocumentBinding) {
	writer.count(len(values))
	for _, binding := range values {
		writer.owner(binding.Owner())
		writer.strings(binding.Destination().String())
		writer.path(binding.PathAuthority())
	}
}
