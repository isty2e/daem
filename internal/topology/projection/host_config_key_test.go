package projection

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestHostConfigKeyIdentityPreservesNativeNames(t *testing.T) {
	keys := [][]string{
		{""},
		{"."},
		{".."},
		{"a/b"},
		{"a", "b"},
		{"~1"},
		{"~"},
		{"a.b"},
		{"%2F"},
		{"\\"},
		{"\x00\n\t"},
		{" \u202e "},
		{"한국어", "日", ""},
		{"01"},
		{"1"},
		{"providers", "0", "apiKey"},
		strings.Split(strings.Repeat("a/", 200)+"", "/"),
	}
	ids := []string{"review", "review:one", "a/b", "a.b", "\x00\n\u202e"}
	seen := make(map[topology.SubjectID]string)
	for nameIndex, name := range ids {
		id, err := entity.New(entity.KindHostConfig, name)
		if err != nil {
			t.Fatal(err)
		}
		for keyIndex, segments := range keys {
			key, err := output.NewKeyPath(segments...)
			if err != nil {
				t.Fatal(err)
			}
			for _, selected := range target.SupportedTargets() {
				for _, scope := range target.SupportedScopes() {
					subject, err := HostConfigKeySubject(id, selected, scope, key)
					if err != nil {
						t.Fatalf("name %d key %d: %v", nameIndex, keyIndex, err)
					}
					caseID := fmt.Sprintf("%d/%d/%s/%s", nameIndex, keyIndex, selected, scope)
					if previous, duplicate := seen[subject]; duplicate {
						t.Fatalf("identity collision: %s and %s", previous, caseID)
					}
					seen[subject] = caseID
					decodedID, decodedKey, ok := HostConfigKeyIdentity(subject)
					if !ok || decodedID != id || decodedKey != key {
						t.Fatalf("lost identity at %s", caseID)
					}
					serialized, err := topology.ParseSubjectID(subject.String())
					if err != nil || serialized != subject {
						t.Fatalf("topology round trip at %s: %v", caseID, err)
					}
					if _, ok := EntityID(subject); ok {
						t.Fatalf("composite key was misread as an entity at %s", caseID)
					}
				}
			}
		}
	}
}

func TestHostConfigKeyIdentityRefusesForgedOrNoncanonicalSubjects(t *testing.T) {
	encode := func(text string) string { return base64.RawURLEncoding.EncodeToString([]byte(text)) }
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	valid := "key-v1." + encode(id.String()) + "." + encode("/a")
	for index, key := range []string{
		"key-v1", valid + ".", valid + "=", strings.Replace(valid, "key-v1", "key-v2", 1),
		"key-v1.." + encode("/a"), "key-v1." + encode(id.String()) + ".",
		"key-v1." + encode(id.String()) + "." + encode("/bad~2escape"),
		"key-v1." + encode("skill:review") + "." + encode("/a"),
		"key-v1." + encode("host_config:%72eview") + "." + encode("/a"),
		valid[:len(valid)-1] + "F", // same decoded bytes, nonzero base64 padding bits
	} {
		subject, err := topology.NewSubjectID(topology.SubjectProjection, "pi.global.host-config", key)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, ok := HostConfigKeyIdentity(subject); ok {
			t.Fatalf("accepted malformed case %d", index)
		}
	}
	for _, namespace := range []string{"pi.global.other", "pi.invalid.host-config", "unsupported.global.host-config", "pi.global.host-config.extra"} {
		subject, err := topology.NewSubjectID(topology.SubjectProjection, namespace, valid)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, ok := HostConfigKeyIdentity(subject); ok {
			t.Fatalf("accepted invalid namespace %s", namespace)
		}
	}
	resource, err := topology.NewSubjectID(topology.SubjectResource, "pi.global.host-config", valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := HostConfigKeyIdentity(resource); ok {
		t.Fatal("accepted resource as a native key projection")
	}
	if _, _, ok := HostConfigKeyIdentity(topology.SubjectID{}); ok {
		t.Fatal("accepted zero identity")
	}
}

func TestHostConfigKeyConstructionRefusesRootAndWrongFamily(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, output.KeyPath{}); err == nil {
		t.Fatal("root selection became key ownership")
	}
	if _, err := HostConfigKeySubject(id, "unsupported", target.ScopeGlobal, key); err == nil {
		t.Fatal("accepted unknown host")
	}
	if _, err := HostConfigKeySubject(id, target.TargetPi, "invalid", key); err == nil {
		t.Fatal("accepted invalid scope")
	}
	wrong, err := entity.New(entity.KindSkill, "review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HostConfigKeySubject(wrong, target.TargetPi, target.ScopeGlobal, key); err == nil {
		t.Fatal("accepted other desired family")
	}
}
