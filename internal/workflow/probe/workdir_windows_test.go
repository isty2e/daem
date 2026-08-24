//go:build windows

package probe

import (
	"strings"
	"testing"
)

func TestWindowsProjectWorkingDirectoryFailsBeforeRootCapture(t *testing.T) {
	binding, err := projectWorkingDirectoryBinder(`Z:\path-that-must-not-be-opened`)()
	if binding != nil {
		_ = binding.Close()
		t.Fatal("unsupported Windows project working directory returned a binding")
	}
	if err == nil || !strings.Contains(err.Error(), "descriptor-backed working directories are unsupported") {
		t.Fatalf("project working-directory error = %v, want platform unsupported", err)
	}
}
