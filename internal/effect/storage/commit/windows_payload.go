//go:build windows

package commit

import (
	"context"
	"fmt"
	"io"

	"golang.org/x/sys/windows"
)

const windowsPayloadChunkSize = 1 << 20

type windowsFlushPolicy struct {
	directory bool
}

func flushWindowsHandle(handle windows.Handle, policy windowsFlushPolicy) error {
	if handle == 0 || handle == windows.InvalidHandle {
		return fmt.Errorf("Windows flush handle is required")
	}
	standard, err := queryWindowsStandardFacts(handle)
	if err != nil {
		return normalizeWindowsNativeError(windowsNativePhaseFlush, err, false)
	}
	if policy.directory != standard.directory {
		if policy.directory {
			return fmt.Errorf("directory flush policy requires a directory handle")
		}
		return fmt.Errorf("file flush policy requires a non-directory handle")
	}
	if err := windows.FlushFileBuffers(handle); err != nil {
		return normalizeWindowsNativeError(windowsNativePhaseFlush, err, false)
	}
	return nil
}

func writeWindowsPayload(ctx context.Context, handle windows.Handle, payload []byte) error {
	if ctx == nil {
		return fmt.Errorf("Windows payload context is required")
	}
	if handle == 0 || handle == windows.InvalidHandle {
		return fmt.Errorf("Windows payload handle is required")
	}
	for len(payload) != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunkLength := len(payload)
		if chunkLength > windowsPayloadChunkSize {
			chunkLength = windowsPayloadChunkSize
		}
		var written uint32
		if err := windows.WriteFile(handle, payload[:chunkLength], &written, nil); err != nil {
			return normalizeWindowsNativeError(windowsNativePhaseWrite, err, false)
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		if int(written) > chunkLength {
			return fmt.Errorf("Windows write returned an invalid count %d", written)
		}
		payload = payload[written:]
	}
	return nil
}

func readWindowsPayloadUpTo(ctx context.Context, handle windows.Handle, maximumBytes int64) ([]byte, error) {
	return readWindowsPayloadWithFaults(ctx, handle, maximumBytes, faultPlan{})
}

func readWindowsPayloadWithFaults(ctx context.Context, handle windows.Handle, maximumBytes int64, faults faultPlan) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Windows payload context is required")
	}
	if handle == 0 || handle == windows.InvalidHandle {
		return nil, fmt.Errorf("Windows payload handle is required")
	}
	if maximumBytes < 0 {
		return nil, fmt.Errorf("Windows payload maximum bytes must not be negative")
	}
	standard, err := queryWindowsStandardFacts(handle)
	if err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseRead, err, false)
	}
	if standard.directory {
		return nil, fmt.Errorf("Windows payload handle is a directory")
	}
	if standard.endOfFile < 0 || standard.endOfFile > maximumBytes {
		observed := standard.endOfFile
		if observed < 0 {
			observed = maximumBytes + 1
		}
		return nil, newRegularFileReadLimitError(maximumBytes, observed)
	}
	if _, err := windows.Seek(handle, 0, io.SeekStart); err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseRead, err, false)
	}
	if standard.endOfFile > int64(int(^uint(0)>>1)) {
		return nil, windowsNativeUnsupported(windowsNativePhaseRead, "bounded payload exceeds the host allocation limit", nil)
	}
	content := make([]byte, 0, int(standard.endOfFile))
	for {
		if err := faults.check(ctx, phaseReadPayload); err != nil {
			return nil, err
		}
		remaining := maximumBytes - int64(len(content))
		if remaining == 0 {
			break
		}
		chunk := make([]byte, int(min(int64(windowsPayloadChunkSize), remaining)))
		read, err := faults.readPayload(chunk, func(value []byte) (int, error) {
			var count uint32
			if err := windows.ReadFile(handle, value, &count, nil); err != nil {
				return int(count), normalizeWindowsNativeError(windowsNativePhaseRead, err, false)
			}
			return int(count), nil
		})
		if err != nil {
			return nil, err
		}
		if read == 0 {
			break
		}
		content = append(content, chunk[:read]...)
		if read < len(chunk) {
			break
		}
	}
	final, err := queryWindowsStandardFacts(handle)
	if err != nil {
		return nil, normalizeWindowsNativeError(windowsNativePhaseRead, err, false)
	}
	if final.endOfFile > maximumBytes {
		return nil, newRegularFileReadLimitError(maximumBytes, final.endOfFile)
	}
	if int64(len(content)) != standard.endOfFile || int64(len(content)) != final.endOfFile {
		return nil, fmt.Errorf("Windows payload size changed during bounded read")
	}
	return content, nil
}
