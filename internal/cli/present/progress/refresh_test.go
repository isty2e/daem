package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRefreshProgressRendererShowsBoundedEscapedFactsAndClears(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	renderer := NewRefreshProgressRenderer(&output)
	renderer.Start("formatter\n\x1b[31m", 10*time.Minute)
	renderer.Close()

	rendered := output.String()
	if !strings.Contains(
		rendered,
		`Refreshing extension formatter\n\x1b[31m (timeout 10m0s)`,
	) {
		t.Fatalf("output = %q", rendered)
	}
	if strings.Count(rendered, "\n") != 0 ||
		!strings.HasSuffix(rendered, "\r\x1b[2K") {
		t.Fatalf("output = %q, want one cleared ephemeral line", rendered)
	}
}

func TestRefreshProgressRendererNilReceiverIsNoop(t *testing.T) {
	t.Parallel()

	var renderer *RefreshProgressRenderer
	renderer.Start("formatter", 10*time.Minute)
	renderer.Close()
}
