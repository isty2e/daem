//go:build unix

package diagnose

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/findings"
)

func TestConfigFileCheckRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	result := make(chan findings.Check, 1)
	go func() {
		result <- configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
			Path:                path,
			Format:              ConfigFormatTOML,
			SyntaxErrorSeverity: findings.SeverityError,
		})
	}()

	select {
	case check := <-result:
		if check.Status != findings.CheckError || !strings.Contains(check.Detail, "not a regular file") {
			t.Fatalf("check = %#v, want non-regular-file error", check)
		}
	case <-time.After(time.Second):
		t.Fatal("config file check blocked on FIFO")
	}
}
