package progress

import (
	"fmt"
	"io"
	"time"
)

// RefreshProgressRenderer renders one fact-based in-flight refresh line.
type RefreshProgressRenderer struct {
	line ephemeralLine
}

// NewRefreshProgressRenderer constructs a refresh progress renderer.
func NewRefreshProgressRenderer(output io.Writer) *RefreshProgressRenderer {
	return &RefreshProgressRenderer{line: newEphemeralLine(output)}
}

// Start shows the selected extension and exact authorized host-command timeout.
func (renderer *RefreshProgressRenderer) Start(
	extensionID string,
	timeout time.Duration,
) {
	if renderer == nil {
		return
	}
	renderer.line.write(fmt.Sprintf(
		"Refreshing extension %s (timeout %s)",
		escapeText(extensionID),
		timeout,
	))
}

// Close clears the active ephemeral progress line.
func (renderer *RefreshProgressRenderer) Close() {
	if renderer == nil {
		return
	}
	renderer.line.close()
}
