package progress

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

type ephemeralLine struct {
	output   io.Writer
	disabled bool
	active   bool
	lastLine string
}

func newEphemeralLine(output io.Writer) ephemeralLine {
	return ephemeralLine{output: output}
}

func (line *ephemeralLine) write(value string) {
	if line.disabled || line.output == nil || value == line.lastLine {
		return
	}
	if _, err := fmt.Fprintf(line.output, "\r\x1b[2K%s", value); err != nil {
		line.disabled = true
		return
	}
	line.active = true
	line.lastLine = value
}

func (line *ephemeralLine) close() {
	if line.disabled || line.output == nil || !line.active {
		return
	}
	if _, err := io.WriteString(line.output, "\r\x1b[2K"); err != nil {
		line.disabled = true
	}
	line.active = false
	line.lastLine = ""
}

func escapeText(value string) string {
	quoted := strconv.QuoteToASCII(strings.ToValidUTF8(value, "\\ufffd"))
	return strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
}
