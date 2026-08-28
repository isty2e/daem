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

func TestViewRemainsComparableWithImmutableRootAuthority(t *testing.T) {
	root := resolvedAccessTestRoot(t)
	view, err := OpenNoFollowView(root)
	if err != nil {
		t.Fatal(err)
	}
	views := map[View]struct{}{view: {}}
	if _, exists := views[view]; !exists {
		t.Fatal("comparable View key was not retained")
	}
	separatelyCaptured, err := OpenNoFollowView(root)
	if err != nil {
		t.Fatal(err)
	}
	if view != separatelyCaptured {
		t.Fatal("separately captured views of one stable root must remain equal")
	}
}

func TestNativePathWitnessDistinguishesObjectIncarnationAndMount(t *testing.T) {
	base := nativePathComponentIdentity{
		device:          1,
		inode:           2,
		kind:            unix.S_IFDIR,
		generation:      3,
		birthTimeSecond: 4,
		birthTimeNano:   5,
		mount:           nativeMountIdentity{first: 6, second: 7},
	}
	for _, mutation := range []func(*nativePathComponentIdentity){
		func(identity *nativePathComponentIdentity) { identity.device++ },
		func(identity *nativePathComponentIdentity) { identity.inode++ },
		func(identity *nativePathComponentIdentity) { identity.kind = unix.S_IFREG },
		func(identity *nativePathComponentIdentity) { identity.generation++ },
		func(identity *nativePathComponentIdentity) { identity.birthTimeSecond++ },
		func(identity *nativePathComponentIdentity) { identity.birthTimeNano++ },
		func(identity *nativePathComponentIdentity) { identity.mount.first++ },
		func(identity *nativePathComponentIdentity) { identity.mount.second++ },
	} {
		changed := base
		mutation(&changed)
		if changed == base {
			t.Fatal("path witness identity mutation was not observable")
		}
	}
}

func TestBoundedNativePathComponentsAcceptsLimitAndRejectsOverflow(t *testing.T) {
	atLimit := strings.Repeat("a/", maximumNativePathComponents-1) + "a"
	components, err := boundedNativePathComponents(atLimit)
	if err != nil {
		t.Fatalf("exact component limit: %v", err)
	}
	if len(components) != maximumNativePathComponents {
		t.Fatalf("component count = %d, want %d", len(components), maximumNativePathComponents)
	}
	overflow := atLimit + "/a"
	if _, err := boundedNativePathComponents(overflow); err == nil ||
		!strings.Contains(err.Error(), "exceeds 4096 components") {
		t.Fatalf("overflow error = %v, want bounded component rejection", err)
	}
}

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

	names, err := readNativeDirectoryNamesUpTo(t.Context(), directoryFD, -1)
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
