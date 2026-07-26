package subprocess

import (
	"slices"
	"testing"
)

func TestSnapshotPreservesEnvironmentAndClassifiesSensitiveValues(t *testing.T) {
	original := []string{
		"HOME=/home/operator",
		"NPM_TOKEN=ambient-token",
		"DATABASE_URL=postgres://user:password@example.test/db",
		"TERM_SESSION_ID=not-classified-by-substring",
		"MALFORMED",
	}
	snapshot := ChildEnvironmentFrom(original)
	original[0] = "HOME=mutated"

	if got := snapshot.Entries(); !slices.Equal(got, []string{
		"HOME=/home/operator",
		"NPM_TOKEN=ambient-token",
		"DATABASE_URL=postgres://user:password@example.test/db",
		"TERM_SESSION_ID=not-classified-by-substring",
		"MALFORMED",
	}) {
		t.Fatalf("Entries = %#v", got)
	}
	if got := snapshot.SecretValues(); !slices.Equal(got, []string{
		"ambient-token",
		"postgres://user:password@example.test/db",
	}) {
		t.Fatalf("SecretValues = %#v", got)
	}
}

func TestWithSecretReplacesChildEntryAndDoesNotMutatePriorSnapshot(t *testing.T) {
	original := ChildEnvironmentFrom([]string{
		"CHILD_VALUE=old",
		"HOST_TOKEN=inherited-secret",
	})
	updated := original.WithSecret("CHILD_VALUE", "declared-secret")

	if got := original.Entries(); !slices.Equal(got, []string{
		"CHILD_VALUE=old",
		"HOST_TOKEN=inherited-secret",
	}) {
		t.Fatalf("original Entries = %#v", got)
	}
	if got := updated.Entries(); !slices.Equal(got, []string{
		"HOST_TOKEN=inherited-secret",
		"CHILD_VALUE=declared-secret",
	}) {
		t.Fatalf("updated Entries = %#v", got)
	}
	if got := updated.SecretValues(); !slices.Equal(got, []string{
		"declared-secret",
		"inherited-secret",
	}) {
		t.Fatalf("updated SecretValues = %#v", got)
	}
}

func TestWithSecretKeepsOnlyLatestExplicitValueForOneChildName(t *testing.T) {
	snapshot := ChildEnvironmentFrom(nil).
		WithSecret("CHILD_VALUE", "first-secret").
		WithSecret("CHILD_VALUE", "second-secret")

	if got := snapshot.Entries(); !slices.Equal(got, []string{"CHILD_VALUE=second-secret"}) {
		t.Fatalf("Entries = %#v", got)
	}
	if got := snapshot.SecretValues(); !slices.Equal(got, []string{"second-secret"}) {
		t.Fatalf("SecretValues = %#v", got)
	}
}

func TestSnapshotSensitiveNameBoundaries(t *testing.T) {
	snapshot := ChildEnvironmentFrom([]string{
		"TOKENIZERS_PARALLELISM=false",
		"NPM_CONFIG__AUTH=encoded-auth",
		"SENTRY_DSN=https://public:secret@example.test/1",
		"lowercase_token=lowercase-secret",
		"TERM_SESSION_ID=ordinary-session-id",
	})

	if got := snapshot.SecretValues(); !slices.Equal(got, []string{
		"encoded-auth",
		"https://public:secret@example.test/1",
		"lowercase-secret",
	}) {
		t.Fatalf("SecretValues = %#v", got)
	}
}

func TestWithSecretReplacesDuplicateNameWithoutTouchingPrefixCollision(t *testing.T) {
	snapshot := ChildEnvironmentFrom([]string{
		"CHILD=first",
		"CHILD_EXTRA=preserved",
		"CHILD=second",
	}).WithSecret("CHILD", "value=with=separators")

	if got := snapshot.Entries(); !slices.Equal(got, []string{
		"CHILD_EXTRA=preserved",
		"CHILD=value=with=separators",
	}) {
		t.Fatalf("Entries = %#v", got)
	}
	if got := snapshot.SecretValues(); !slices.Equal(got, []string{"value=with=separators"}) {
		t.Fatalf("SecretValues = %#v", got)
	}
}

func TestSnapshotSecretValuesDeduplicateAndIgnoreEmptyValues(t *testing.T) {
	snapshot := ChildEnvironmentFrom([]string{
		"FIRST_TOKEN=shared-secret",
		"SECOND_SECRET=shared-secret",
		"EMPTY_PASSWORD=",
	}).WithSecret("EXPLICIT", "")

	if got := snapshot.SecretValues(); !slices.Equal(got, []string{"shared-secret"}) {
		t.Fatalf("SecretValues = %#v", got)
	}
}
