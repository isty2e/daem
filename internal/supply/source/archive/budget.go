package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	defaultInputBytes      int64 = 256 << 20
	defaultTarStreamBytes  int64 = 768 << 20
	defaultExpandedBytes   int64 = 512 << 20
	defaultEntryBytes      int64 = 128 << 20
	defaultEntryCount      int64 = 100_000
	defaultPathBytes       int64 = 4_096
	defaultPathDepth       int64 = 64
	maximumDiagnosticBytes       = 128
)

// LimitKind identifies one independent archive resource dimension.
type LimitKind string

const (
	LimitInputBytes     LimitKind = "input_bytes"
	LimitTarStreamBytes LimitKind = "tar_stream_bytes"
	LimitExpandedBytes  LimitKind = "expanded_bytes"
	LimitEntryBytes     LimitKind = "entry_bytes"
	LimitEntryCount     LimitKind = "entry_count"
	LimitPathBytes      LimitKind = "path_bytes"
	LimitPathDepth      LimitKind = "path_depth"
)

// ErrLimitExceeded classifies bounded archive extraction failures.
var ErrLimitExceeded = errors.New("archive extraction limit exceeded")

// LimitError reports one exhausted archive resource dimension.
type LimitError struct {
	kind     LimitKind
	limit    int64
	observed int64
	entry    string
}

func (err *LimitError) Error() string {
	if err == nil {
		return ErrLimitExceeded.Error()
	}
	detail := fmt.Sprintf("%s: %s observed=%d limit=%d", ErrLimitExceeded, err.kind, err.observed, err.limit)
	if err.entry != "" {
		detail += fmt.Sprintf(" entry=%q", err.entry)
	}
	return detail
}

func (err *LimitError) Unwrap() error {
	return ErrLimitExceeded
}

// Kind returns the exhausted resource dimension.
func (err *LimitError) Kind() LimitKind {
	if err == nil {
		return ""
	}
	return err.kind
}

// Limit returns the configured maximum for the exhausted dimension.
func (err *LimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.limit
}

// Observed returns the first observed value known to exceed the limit.
func (err *LimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

// Entry returns a bounded archive entry diagnostic when one is relevant.
func (err *LimitError) Entry() string {
	if err == nil {
		return ""
	}
	return err.entry
}

type budget struct {
	inputBytes     int64
	tarStreamBytes int64
	expandedBytes  int64
	entryBytes     int64
	entryCount     int64
	pathBytes      int64
	pathDepth      int64
}

func defaultBudget() budget {
	return budget{
		inputBytes:     defaultInputBytes,
		tarStreamBytes: defaultTarStreamBytes,
		expandedBytes:  defaultExpandedBytes,
		entryBytes:     defaultEntryBytes,
		entryCount:     defaultEntryCount,
		pathBytes:      defaultPathBytes,
		pathDepth:      defaultPathDepth,
	}
}

// CheckInputSize rejects a known transport size above the package-owned input budget.
// Streaming extraction still enforces the same limit when the value is absent
// or inaccurate.
func CheckInputSize(size int64) error {
	return defaultBudget().checkInputSize(size)
}

func (budget budget) checkInputSize(size int64) error {
	if err := budget.validate(); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("archive input size must not be negative")
	}
	if size > budget.inputBytes {
		return newLimitError(LimitInputBytes, budget.inputBytes, size, "")
	}
	return nil
}

func (budget budget) validate() error {
	if budget.inputBytes <= 0 || budget.tarStreamBytes <= 0 || budget.expandedBytes <= 0 ||
		budget.entryBytes <= 0 || budget.entryCount <= 0 || budget.pathBytes <= 0 || budget.pathDepth <= 0 {
		return fmt.Errorf("archive budget is not initialized")
	}
	if budget.entryBytes > budget.expandedBytes {
		return fmt.Errorf("archive entry budget exceeds total expanded budget")
	}
	return nil
}

func newLimitError(kind LimitKind, limit int64, observed int64, entry string) *LimitError {
	if len(entry) > maximumDiagnosticBytes {
		entry = entry[:maximumDiagnosticBytes-3] + "..."
	}
	return &LimitError{kind: kind, limit: limit, observed: observed, entry: entry}
}

type boundedReader struct {
	ctx      context.Context
	reader   io.Reader
	kind     LimitKind
	limit    int64
	observed int64
	empty    int
}

func (reader *boundedReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return reader.reader.Read(buffer)
	}
	remaining := reader.limit - reader.observed
	readSize := len(buffer)
	if remaining < int64(readSize) {
		readSize = int(remaining) + 1
		if readSize < 1 {
			readSize = 1
		}
	}
	count, err := reader.reader.Read(buffer[:readSize])
	if count == 0 && err == nil {
		reader.empty++
		if reader.empty >= 100 {
			return 0, io.ErrNoProgress
		}
	} else {
		reader.empty = 0
	}
	reader.observed += int64(count)
	if reader.observed > reader.limit {
		return 0, newLimitError(reader.kind, reader.limit, reader.observed, "")
	}
	if contextErr := reader.ctx.Err(); contextErr != nil {
		return count, contextErr
	}
	return count, err
}

func newBoundedReader(ctx context.Context, input io.Reader, kind LimitKind, limit int64) *boundedReader {
	return &boundedReader{ctx: ctx, reader: input, kind: kind, limit: limit}
}
