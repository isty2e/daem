//go:build windows

package filesnapshot

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func readRegularFileAtCounted(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (CountedContent, error) {
	return readRegularFileAtCountedWithHooks(ctx, dir, name, maximumBytes, readHooks{})
}

func readRegularFileAtCountedWithHooks(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
	hooks readHooks,
) (CountedContent, error) {
	if ctx == nil {
		return CountedContent{}, errors.New("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{}, err
	}
	if dir == nil {
		return CountedContent{}, errors.New("file snapshot directory descriptor is required")
	}
	if maximumBytes <= 0 {
		return CountedContent{}, errors.New("maximum file size must be positive")
	}
	if err := validDirentName(name); err != nil {
		return CountedContent{}, err
	}

	file, err := OpenEntryAt(dir, name)
	if errors.Is(err, os.ErrNotExist) {
		return CountedContent{}, nil
	}
	if err != nil {
		return CountedContent{}, err
	}
	defer file.Close()

	beforeInfo, err := file.Stat()
	if err != nil {
		return CountedContent{}, err
	}
	if !beforeInfo.Mode().IsRegular() {
		return CountedContent{}, ErrNotRegular
	}
	if beforeInfo.Size() > maximumBytes {
		return CountedContent{}, limitError(maximumBytes)
	}
	beforeID, err := queryWindowsFileID(windows.Handle(file.Fd()))
	if err != nil {
		return CountedContent{}, err
	}
	beforeState, err := queryWindowsFileBasicInfo(windows.Handle(file.Fd()))
	if err != nil {
		return CountedContent{}, err
	}
	if hooks.afterInspect != nil {
		hooks.afterInspect()
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{}, err
	}

	content, attempted, err := readBoundedRegularFile(ctx, file, maximumBytes, beforeInfo.Size())
	if err != nil {
		return CountedContent{Attempted: attempted}, err
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{Attempted: attempted}, err
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return CountedContent{Attempted: attempted}, err
	}
	afterID, err := queryWindowsFileID(windows.Handle(file.Fd()))
	if err != nil {
		return CountedContent{Attempted: attempted}, err
	}
	afterState, err := queryWindowsFileBasicInfo(windows.Handle(file.Fd()))
	if err != nil {
		return CountedContent{Attempted: attempted}, err
	}

	current, err := OpenEntryAt(dir, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrSymlink) {
			return CountedContent{Attempted: attempted}, ErrChanged
		}
		return CountedContent{Attempted: attempted}, err
	}
	currentID, currentErr := queryWindowsFileID(windows.Handle(current.Fd()))
	closeErr := current.Close()
	if currentErr != nil {
		return CountedContent{Attempted: attempted}, currentErr
	}
	if closeErr != nil {
		return CountedContent{Attempted: attempted}, closeErr
	}
	if beforeID != afterID || beforeID != currentID || beforeState != afterState ||
		beforeInfo.Size() != afterInfo.Size() || int64(len(content)) != afterInfo.Size() {
		return CountedContent{Attempted: attempted}, ErrChanged
	}
	return CountedContent{Content: content, Exists: true, Attempted: attempted}, nil
}
