package focusedprobe

import (
	"os"
	"testing"
	"time"
)

func TestFocusedCacheProbe(t *testing.T) {
	if os.Getenv("GOENV") != "off" || os.Getenv("GOWORK") != "off" {
		t.Fatalf("Go policy was not normalized: GOENV=%q GOWORK=%q", os.Getenv("GOENV"), os.Getenv("GOWORK"))
	}
	if _, present := os.LookupEnv("GOFLAGS"); present {
		t.Fatal("GOFLAGS must be absent")
	}
	if fixture := os.Getenv("DAEM_FOCUSED_PROBE_FIXTURE"); fixture != "" {
		if _, err := os.ReadFile(fixture); err != nil {
			t.Fatalf("read focused fixture: %v", err)
		}
	}
	_ = os.Getenv("DAEM_FOCUSED_PROBE_VALUE")

	ready := os.Getenv("DAEM_FOCUSED_PROBE_READY")
	release := os.Getenv("DAEM_FOCUSED_PROBE_RELEASE")
	if ready == "" || release == "" {
		return
	}
	if err := os.WriteFile(ready, []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("write ready marker: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(release); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("observe release marker: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for focused probe release")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
