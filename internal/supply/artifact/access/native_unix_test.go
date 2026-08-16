//go:build darwin || linux

package access

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeIdentitySameBindingIgnoresMutableDirectoryMetadata(t *testing.T) {
	base := nativeIdentity{
		device:           1,
		inode:            2,
		changeTimeSecond: 3,
		changeTimeNano:   4,
		mode:             unix.S_IFDIR | 0o700,
		size:             5,
	}
	changedDirectory := base
	changedDirectory.changeTimeSecond++
	changedDirectory.size++

	if !base.sameBinding(changedDirectory) {
		t.Fatal("directory metadata change must preserve path-component binding")
	}
	if base.equal(changedDirectory) {
		t.Fatal("strict artifact identity must detect directory metadata change")
	}

	replaced := changedDirectory
	replaced.inode++
	if base.sameBinding(replaced) {
		t.Fatal("inode replacement must change path-component binding")
	}
	retyped := changedDirectory
	retyped.mode = unix.S_IFREG | 0o600
	if base.sameBinding(retyped) {
		t.Fatal("entry-kind replacement must change path-component binding")
	}
}

func TestVerifyNativeDirectoryNamesDetectsExactNameChange(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "Skill.md")
	if err := os.WriteFile(original, []byte("content\n"), 0o600); err != nil {
		t.Fatalf("write original entry: %v", err)
	}
	directoryFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open directory: %v", err)
	}
	defer func() {
		if err := unix.Close(directoryFD); err != nil {
			t.Errorf("close directory: %v", err)
		}
	}()

	names, err := readNativeDirectoryNames(directoryFD)
	if err != nil {
		t.Fatalf("read initial names: %v", err)
	}
	if err := os.Rename(original, filepath.Join(root, "SKILL.md")); err != nil {
		t.Fatalf("change exact entry name: %v", err)
	}

	err = verifyNativeDirectoryNames(context.Background(), directoryFD, names)
	if err == nil || !strings.Contains(err.Error(), "directory entries changed") {
		t.Fatalf("verify names error = %v, want exact-name change", err)
	}
}

func TestReadNativeDirectoryNamesUpToChargesOneOverflowProbe(t *testing.T) {
	root := t.TempDir()
	for index := range 4 {
		path := filepath.Join(root, fmt.Sprintf("entry-%d", index))
		if err := os.WriteFile(path, []byte("content\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directoryFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(directoryFD); err != nil {
			t.Errorf("close directory: %v", err)
		}
	}()

	names, err := readNativeDirectoryNamesUpTo(context.Background(), directoryFD, 2)
	if err != nil {
		t.Fatalf("readNativeDirectoryNamesUpTo returned error: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("overflow probe names = %d, want 3", len(names))
	}
}

func TestReadNativeDirectoryNamesUpToHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "entry"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directoryFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(directoryFD); err != nil {
			t.Errorf("close directory: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	names, err := readNativeDirectoryNamesUpTo(ctx, directoryFD, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readNativeDirectoryNamesUpTo error = %v, want context.Canceled", err)
	}
	if names != nil {
		t.Fatalf("cancelled lookup names = %#v, want nil", names)
	}
}
