package outputtest

import "testing"

// LegacyContentPath requires the model's own lossless boundary conversion for a fixture.
func LegacyContentPath[T ~string](t testing.TB, value interface{ LegacyContentPath() (T, error) }) T {
	t.Helper()
	path, err := value.LegacyContentPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}
