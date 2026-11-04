package projection

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// HostConfigKeySubject identifies an atomic native value independently of its
// content, document spelling, codec version or execution occurrence.
func HostConfigKeySubject(id entity.ID, selected target.Target, scope target.Scope, key output.KeyPath) (topology.SubjectID, error) {
	if err := id.Validate(); err != nil {
		return topology.SubjectID{}, err
	}
	if id.Kind() != entity.KindHostConfig || key.IsRoot() {
		return topology.SubjectID{}, fmt.Errorf("native key projection requires a host_config entity and a non-root key")
	}
	namespace, err := hostConfigKeyNamespace(selected, scope)
	if err != nil {
		return topology.SubjectID{}, err
	}

	// The encoded fields retain native control characters without relaxing the
	// shared SubjectID grammar or treating an encoded key as an ownership path.
	encoded := "key-v1." + base64.RawURLEncoding.EncodeToString([]byte(id.String())) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(key.String()))
	return topology.NewSubjectID(topology.SubjectProjection, namespace, encoded)
}

// HostConfigKeyIdentity inverts only canonical native-key subjects. EntityID
// intentionally continues to reject this composite identity grammar.
func HostConfigKeyIdentity(subject topology.SubjectID) (entity.ID, output.KeyPath, bool) {
	if subject.Validate() != nil || subject.Kind() != topology.SubjectProjection {
		return entity.ID{}, output.KeyPath{}, false
	}
	namespace := strings.Split(subject.Namespace(), ".")
	if len(namespace) != 3 || namespace[2] != "host-config" {
		return entity.ID{}, output.KeyPath{}, false
	}
	selected, scope := target.Target(namespace[0]), target.Scope(namespace[1])
	if _, err := hostConfigKeyNamespace(selected, scope); err != nil {
		return entity.ID{}, output.KeyPath{}, false
	}
	parts := strings.Split(subject.Key(), ".")
	if len(parts) != 3 || parts[0] != "key-v1" {
		return entity.ID{}, output.KeyPath{}, false
	}
	idBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return entity.ID{}, output.KeyPath{}, false
	}
	keyBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return entity.ID{}, output.KeyPath{}, false
	}
	id, err := entity.Parse(string(idBytes))
	if err != nil {
		return entity.ID{}, output.KeyPath{}, false
	}
	key, err := output.ParseKeyPath(string(keyBytes))
	if err != nil {
		return entity.ID{}, output.KeyPath{}, false
	}
	canonical, err := HostConfigKeySubject(id, selected, scope, key)
	if err != nil || canonical != subject {
		return entity.ID{}, output.KeyPath{}, false
	}
	return id, key, true
}

func hostConfigKeyNamespace(selected target.Target, scope target.Scope) (string, error) {
	if selected != target.TargetPi && selected != target.TargetCodex {
		return "", fmt.Errorf("native key projection target is unsupported")
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return "", err
	}
	return string(selected) + "." + string(scope) + ".host-config", nil
}
