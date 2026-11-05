package preparation

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization"
)

func ManagedPathPayload(effect ManagedPathEffect, payloads payload.PayloadSet) (payload.Payload, error) {
	prepared, ok := payloads.LookupSubject(effect.Subject())
	if !ok {
		return payload.Payload{}, fmt.Errorf(
			"missing managed path payload for subject %s/%s %q",
			effect.Subject().Kind(), effect.Subject().Namespace(), effect.Subject().Key(),
		)
	}
	if err := prepared.VerifyHash(effect.DesiredHash(), effect.Destination()); err != nil {
		return payload.Payload{}, err
	}
	switch effect.ContentKind() {
	case realization.PathProjectionFile:
		file, isFile := prepared.File()
		if !isFile {
			return payload.Payload{}, fmt.Errorf("managed file effect for %q has a non-file payload", effect.Destination())
		}
		if file.Mode().Perm() != effect.DesiredFileMode().Perm() {
			return payload.Payload{}, fmt.Errorf(
				"managed file payload mode %04o does not match planned mode %04o for %q",
				file.Mode().Perm(),
				effect.DesiredFileMode().Perm(),
				effect.Destination(),
			)
		}
	case realization.PathProjectionDirectory:
		if _, isDirectory := prepared.Directory(); !isDirectory {
			return payload.Payload{}, fmt.Errorf("managed directory effect for %q has a non-directory payload", effect.Destination())
		}
	default:
		return payload.Payload{}, fmt.Errorf("managed path content kind %q is unsupported", effect.ContentKind())
	}
	return prepared, nil
}
