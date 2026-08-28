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
	build := func(identity nativePathComponentIdentity) nativePathWitness {
		builder := newNativePathWitnessBuilder()
		builder.append(identity)
		return builder.finish()
	}
	baseWitness := build(base)
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
		if changedWitness := build(changed); changedWitness == baseWitness {
			t.Fatalf("path witness did not encode identity mutation: %#v", changed)
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

func TestDirectoryListingIdentityIncludesRelativeAuthority(t *testing.T) {
	native := nativeIdentity{
		device:           1,
		inode:            2,
		changeTimeSecond: 3,
		changeTimeNano:   4,
		mode:             unix.S_IFDIR | 0o700,
		size:             5,
	}
	firstBuilder := newNativePathWitnessBuilder()
	firstBuilder.append(nativePathComponentIdentity{device: 1, inode: 10, kind: unix.S_IFDIR})
	secondBuilder := newNativePathWitnessBuilder()
	secondBuilder.append(nativePathComponentIdentity{device: 1, inode: 11, kind: unix.S_IFDIR})
	first := directoryListingIdentity{native: native, relative: firstBuilder.finish()}
	second := directoryListingIdentity{native: native, relative: secondBuilder.finish()}

	if first.equal(second) {
		t.Fatal("listing identities with different relative authority compared equal")
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

func TestOpenNativeRelativeRejectsReplacementAfterExactNameObservation(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "selected")
	writeAccessTestFile(t, filepath.Join(selected, "original"), []byte("original\n"))
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(rootFD); err != nil {
			t.Errorf("close root: %v", err)
		}
	}()

	replaced := false
	observeExactName := func(
		ctx context.Context,
		parentFD int,
		name string,
	) (nativeExactNameBinding, bool, error) {
		binding, exact, err := observeNativeExactNameBinding(ctx, parentFD, name)
		if err != nil || !exact || replaced {
			return binding, exact, err
		}
		replaced = true
		moved := filepath.Join(root, "moved")
		if err := os.Rename(selected, moved); err != nil {
			return nativeExactNameBinding{}, false, err
		}
		writeAccessTestFile(t, filepath.Join(selected, "replacement"), []byte("replacement\n"))
		return binding, true, nil
	}

	entry, err := openNativeRelativeWithExactNameCheck(
		t.Context(),
		rootFD,
		"selected",
		nativePathWitness{},
		observeExactName,
	)
	if entry != nil {
		_ = entry.close()
	}
	if err == nil || !strings.Contains(err.Error(), "changed while open") {
		t.Fatalf("relative open after exact-name replacement error = %v, want opened-entry rejection", err)
	}
}

func TestOpenNativeRelativeRejectsCaseOnlyRenameAfterExactNameObservation(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "selected")
	writeAccessTestFile(t, filepath.Join(selected, "content"), []byte("content\n"))
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Close(rootFD); err != nil {
			t.Errorf("close root: %v", err)
		}
	}()

	observations := 0
	observeExactName := func(
		ctx context.Context,
		parentFD int,
		name string,
	) (nativeExactNameBinding, bool, error) {
		observations++
		if observations > 1 {
			return nativeExactNameBinding{}, false, nil
		}
		return observeNativeExactNameBinding(ctx, parentFD, name)
	}

	entry, err := openNativeRelativeWithExactNameCheck(
		t.Context(),
		rootFD,
		"selected",
		nativePathWitness{},
		observeExactName,
	)
	if entry != nil {
		_ = entry.close()
	}
	if err == nil || !strings.Contains(err.Error(), "exact casing") {
		t.Fatalf("relative open after case-only rename error = %v, want exact-case rejection", err)
	}
	if observations != 2 {
		t.Fatalf("exact-name observations = %d, want before-and-after binding observations", observations)
	}
}

func TestObserveNativeExactNameBindingPreservesCloseError(t *testing.T) {
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

	closeFailure := errors.New("injected directory close failure")
	closeCalls := 0
	_, exact, err := observeNativeExactNameBindingWithIO(
		t.Context(),
		directoryFD,
		"entry",
		func(file *os.File, maximum int) ([]string, error) {
			return file.Readdirnames(maximum)
		},
		func(file *os.File) error {
			closeCalls++
			return errors.Join(file.Close(), closeFailure)
		},
	)
	if !exact {
		t.Fatal("exact name was not reported")
	}
	if !errors.Is(err, closeFailure) {
		t.Fatalf("exact-name close error = %v, want injected close failure", err)
	}
	if closeCalls != 1 {
		t.Fatalf("directory close calls = %d, want 1", closeCalls)
	}
}

func TestObserveNativeExactNameBindingRejectsParentChangeDuringScan(t *testing.T) {
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

	changed := false
	_, _, err = observeNativeExactNameBindingWithIO(
		t.Context(),
		directoryFD,
		"entry",
		func(file *os.File, maximum int) ([]string, error) {
			names, readErr := file.Readdirnames(maximum)
			if !changed {
				changed = true
				if err := os.Chmod(root, 0o750); err != nil {
					return nil, err
				}
			}
			return names, readErr
		},
		func(file *os.File) error {
			return file.Close()
		},
	)
	if err == nil || !strings.Contains(err.Error(), "directory changed during exact-name observation") {
		t.Fatalf("parent-change observation error = %v, want stable-parent rejection", err)
	}
}

func TestObserveNativeExactNameBindingPreservesBatchReadErrorBeforeEarlyStop(t *testing.T) {
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

	readFailure := errors.New("injected directory read failure")
	closeFailure := errors.New("injected directory close failure")
	closeCalls := 0
	_, exact, err := observeNativeExactNameBindingWithIO(
		t.Context(),
		directoryFD,
		"entry",
		func(*os.File, int) ([]string, error) {
			return []string{"entry"}, readFailure
		},
		func(file *os.File) error {
			closeCalls++
			return errors.Join(file.Close(), closeFailure)
		},
	)
	if !exact {
		t.Fatal("exact name was not reported from the errored batch")
	}
	if !errors.Is(err, readFailure) {
		t.Fatalf("early-stop read error = %v, want injected read failure", err)
	}
	if !errors.Is(err, closeFailure) {
		t.Fatalf("early-stop close error = %v, want injected close failure", err)
	}
	if closeCalls != 1 {
		t.Fatalf("directory close calls = %d, want 1", closeCalls)
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
