//go:build darwin || linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
)

func TestUserStateMigrationPreservesInstalledSkillAcrossSelectionModes(t *testing.T) {
	for _, sourceMode := range []string{"vendor", "link"} {
		t.Run(sourceMode, func(t *testing.T) {
			testUserStateMigrationPreservesInstalledSkill(t, sourceMode)
		})
	}
}

func testUserStateMigrationPreservesInstalledSkill(t *testing.T, sourceMode string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.SetDefaultRootEnv(t, root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	manifest := filepath.Join(root, "config", "daem", "daem.toml")
	skill := filepath.Join(root, "skill")
	testkit.WriteFile(t, skill, "SKILL.md", "---\nname: example\ndescription: Isolated migration example.\n---\nExample.\n")
	testkit.WriteFile(t, filepath.Dir(manifest), filepath.Base(manifest), fmt.Sprintf("version = 1\ntargets = [\"pi\"]\n[defaults]\nscope = \"global\"\n[[skill]]\nname = \"example\"\nsource = { path = %q, mode = %q }\n", skill, sourceMode))
	testkit.WithWorkingDirectory(t, root)

	// Ordinary selection populates the former local layout with the real apply
	// producer. Recognizing that same entry as the user manifest models upgrade.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "unused-config"))
	migrationCLI(t, 0, "lock", "--manifest", manifest)
	migrationCLI(t, 0, "apply", "--manifest", manifest, "--yes")
	legacyState := filepath.Join(filepath.Dir(manifest), ".daem", "state.json")
	legacyBefore := testkit.ReadFile(t, legacyState)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	outputs := make(map[string]migrationOutputImage)
	for _, claim := range registry.Claims() {
		captureMigrationOutput(t, claim.Address().Path(), outputs)
	}
	hasPayload := false
	for _, image := range outputs {
		hasPayload = hasPayload || image.mode.IsRegular()
	}
	if !hasPayload {
		t.Fatal("fixture owns no installed payload")
	}
	sourceFile := filepath.Join(skill, "SKILL.md")
	outputs[sourceFile] = readMigrationOutput(t, sourceFile)

	declarationBefore := testkit.ReadFile(t, manifest)
	lockBefore := testkit.ReadFile(t, paths.LockfilePath)

	migrationCLI(t, 1, "status")
	migrationCLI(t, 1, "lock", "--manifest", manifest)
	migrationCLI(t, 2, "migrate", "state", "--manifest", manifest)
	preview := migrationCLI(t, 0, "migrate", "state", "--manifest", manifest, "--dry-run", "--json")
	var envelope struct {
		Action       string `json:"action"`
		Source       string `json:"source_statefile"`
		Destination  string `json:"destination_statefile"`
		OutputClaims int    `json:"output_claims"`
	}
	if err := json.Unmarshal([]byte(preview), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Action != "migrate" || envelope.Source != legacyState || envelope.Destination != paths.StatefilePath || envelope.OutputClaims == 0 {
		t.Fatalf("preview = %s", preview)
	}
	if !bytes.Equal(testkit.ReadFile(t, legacyState), legacyBefore) {
		t.Fatal("preview changed legacy state")
	}
	testkit.AssertPathMissing(t, paths.StatefilePath)
	migrationCLI(t, 0, "migrate", "state", "--manifest", manifest, "--yes", "--json")
	if _, err := statefile.Load(t.Context(), legacyState); err == nil {
		t.Fatal("old reader can use retired state")
	}
	migrationCLI(t, 0, "migrate", "state", "--manifest", manifest, "--yes")

	for _, mode := range []string{"explicit", "cwd", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			cwd := root
			if mode == "cwd" {
				cwd = filepath.Dir(manifest)
			}
			testkit.WithWorkingDirectory(t, cwd)
			statusArgs := []string{"status", "--check"}
			applyArgs := []string{"apply", "--yes"}
			if mode == "explicit" {
				statusArgs = append(statusArgs, "--manifest", manifest)
				applyArgs = append(applyArgs, "--manifest", manifest)
			}
			migrationCLI(t, 0, statusArgs...)
			migrationCLI(t, 0, applyArgs...)
		})
	}
	if !bytes.Equal(testkit.ReadFile(t, manifest), declarationBefore) || !bytes.Equal(testkit.ReadFile(t, paths.LockfilePath), lockBefore) {
		t.Fatal("migration changed declarations")
	}
	for path, before := range outputs {
		after := readMigrationOutput(t, path)
		if !os.SameFile(before.info, after.info) || !reflect.DeepEqual(before.content, after.content) || before.link != after.link || before.mode != after.mode || before.uid != after.uid || before.gid != after.gid {
			t.Fatalf("installed output changed: %s", path)
		}
	}

	// A later authorized source update must use the transferred authority too,
	// not merely succeed while the old installation remains unchanged.
	testkit.WriteFile(t, skill, "SKILL.md", "---\nname: example\ndescription: Updated migration example.\n---\nUpdated.\n")
	migrationCLI(t, 0, "lock", "--manifest", manifest)
	migrationCLI(t, 0, "apply", "--manifest", manifest, "--yes")
	migrationCLI(t, 0, "status", "--check")
}

func migrationCLI(t *testing.T, want int, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := testkit.RunCLI(args, &stdout, &stderr); code != want {
		t.Fatalf("%q: code=%d want=%d\nstdout: %s\nstderr: %s", args, code, want, stdout.String(), stderr.String())
	}
	return stdout.String()
}

type migrationOutputImage struct {
	info     os.FileInfo
	content  []byte
	link     string
	mode     fs.FileMode
	uid, gid uint32
}

func captureMigrationOutput(t *testing.T, root string, images map[string]migrationOutputImage) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		images[path] = readMigrationOutput(t, path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func readMigrationOutput(t *testing.T, path string) migrationOutputImage {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	image := migrationOutputImage{info: info, mode: info.Mode(), uid: stat.Uid, gid: stat.Gid}
	if info.Mode().IsRegular() {
		image.content, err = os.ReadFile(path)
	} else if info.Mode()&os.ModeSymlink != 0 {
		image.link, err = os.Readlink(path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return image
}
