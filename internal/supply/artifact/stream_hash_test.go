package artifact

import (
	"bytes"
	"context"
	"testing"
)

func TestHashFileReaderMatchesCanonicalFileHash(t *testing.T) {
	t.Parallel()

	content := []byte("exact file bytes\n")
	for _, executable := range []bool{false, true} {
		got, err := HashFileReader(context.Background(), bytes.NewReader(content), int64(len(content)), executable)
		if err != nil {
			t.Fatalf("HashFileReader() error = %v", err)
		}
		want := HashFileContentWithExecutable(content, executable)
		if got != want {
			t.Fatalf("HashFileReader() = %q, want %q", got, want)
		}
	}
}

func TestHashFileReaderRejectsLengthDrift(t *testing.T) {
	t.Parallel()

	if _, err := HashFileReader(context.Background(), bytes.NewReader([]byte("short")), 6, false); err == nil {
		t.Fatal("HashFileReader() accepted content shorter than declared size")
	}
	if _, err := HashFileReader(context.Background(), bytes.NewReader([]byte("long")), 3, false); err == nil {
		t.Fatal("HashFileReader() accepted content longer than declared size")
	}
}
