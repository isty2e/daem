package lockfile

import (
	"strings"
	"testing"
)

func TestLoadRejectsNonCanonicalGeneratedLockKeys(t *testing.T) {
	file := lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle"))
	content, err := Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		from string
		to   string
	}{
		{"version envelope", currentLockfileVersionEnvelope(), strings.Replace(currentLockfileVersionEnvelope(), "version", "Version", 1)},
		{"subject field", "entity_id =", "Entity_ID ="},
		{"exact supply field", "source_id =", "Source_ID ="},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := string(content)
			if !strings.Contains(original, test.from) {
				t.Fatalf("generated lock has no %q key", test.from)
			}
			mutated := strings.Replace(original, test.from, test.to, 1)
			if _, err := Load(t.Context(), writeLockfileText(t, mutated)); err == nil {
				t.Fatal("noncanonical generated lock key was accepted")
			}
		})
	}

	if _, err := Load(t.Context(), writeLockfileText(t, string(content))); err != nil {
		t.Fatalf("canonical generated lock rejected: %v", err)
	}
}
