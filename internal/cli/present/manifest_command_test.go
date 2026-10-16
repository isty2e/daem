package clipresent

import (
	"bytes"
	"io"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	initworkflow "github.com/isty2e/daem/internal/workflow/init"
)

func TestManifestCommandPreservesSelectionAndArguments(t *testing.T) {
	for _, test := range []struct {
		name     string
		path     string
		implicit string
		omit     bool
	}{
		{name: "observed same cwd", path: "/project/daem.toml", implicit: "/project/daem.toml", omit: true},
		{name: "no observation", path: "/project/daem.toml"},
		{name: "other workspace", path: "/other/daem.toml", implicit: "/project/daem.toml"},
		{name: "alias is not proof", path: "/alias/daem.toml", implicit: "/project/daem.toml"},
		{name: "case is not proof", path: "/project/DAEM.toml", implicit: "/project/daem.toml"},
		{name: "empty selector remains implicit", omit: true},
		{name: "quoted path", path: "/project 'name; ${HOME}/daem.toml"},
		{name: "terminal safe path", path: "/project\nname\x1b/daem.toml"},
		{name: "invalid UTF8 path", path: string([]byte{'/', 'a', 0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := []string{"remove", "skill", "name with 'quotes"}
			flags := []string{"--target", "codex", "--target", "pi", "--scope", "global", "--dry-run"}
			text, err := ManifestCommand(command, test.path, HumanOptions{ImplicitManifestPath: test.implicit}, flags...)
			if err != nil {
				t.Fatal(err)
			}
			want := slices.Clone(command)
			if !test.omit {
				want = append(want, "--manifest", test.path)
			}
			want = append(want, flags...)
			if got := manifestCommandArgv(t, text); !reflect.DeepEqual(got, want) {
				t.Fatalf("argv=%q, want %q", got, want)
			}
		})
	}
	var output bytes.Buffer
	PrintManifestCommand(&output, "next: run ", []string{"lock"}, "bad\x00path", HumanOptions{})
	if output.Len() != 0 {
		t.Fatalf("unrepresentable command=%q, want no concrete command", &output)
	}
}

func TestManifestHintPresentersUseTheSameProjection(t *testing.T) {
	path := "/project with spaces/daem.toml"
	for _, producer := range []struct {
		name   string
		render func(io.Writer, HumanOptions)
		want   [][]string
	}{
		{name: "init", render: func(output io.Writer, options HumanOptions) {
			PrintInitPlanWithOptions(output, "created", initworkflow.Plan{ManifestPath: path}, options)
		}, want: [][]string{{"lock", "--dry-run"}}},
		{name: "authoring", render: func(output io.Writer, options HumanOptions) {
			PrintAuthoringChangeWithOptions(output, AuthoringChangeSummary{ManifestPath: path}, options)
		}, want: [][]string{{"apply", "--dry-run"}}},
		{name: "import", render: func(output io.Writer, options HumanOptions) {
			PrintImportPlanWithOptions(output, ImportPlan{ManifestPath: path, ResourceCount: 1}, options)
		}, want: [][]string{{"lock", "--dry-run"}, {"apply", "--manage-existing", "--dry-run"}}},
	} {
		t.Run(producer.name, func(t *testing.T) {
			for _, observed := range []string{path, "/different/daem.toml", ""} {
				var output bytes.Buffer
				producer.render(&output, HumanOptions{ImplicitManifestPath: observed})
				var commands [][]string
				for line := range strings.SplitSeq(output.String(), "\n") {
					if command, ok := strings.CutPrefix(line, "next: run "); ok {
						commands = append(commands, manifestCommandArgv(t, command))
					}
				}
				want := make([][]string, len(producer.want))
				for index, args := range producer.want {
					want[index] = []string{args[0]}
					if observed != path {
						want[index] = append(want[index], "--manifest", path)
					}
					want[index] = append(want[index], args[1:]...)
				}
				if !reflect.DeepEqual(commands, want) {
					t.Fatalf("observed=%q next commands=%q, want %q", observed, commands, want)
				}
			}
		})
	}
}

func TestManifestCommandPreservesOriginalSelectorWhenElisionIsUnproven(t *testing.T) {
	for _, request := range []string{"", "./selected/daem.toml", "/selected 'path/daem.toml", "line\nbreak\x1b.toml"} {
		text, err := ManifestCommand([]string{"apply"}, "/resolved/daem.toml", HumanOptions{ManifestRequest: &request}, "--dry-run")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"apply", "--dry-run"}
		if request != "" {
			want = []string{"apply", "--manifest", request, "--dry-run"}
		}
		if got := manifestCommandArgv(t, text); !reflect.DeepEqual(got, want) {
			t.Fatalf("fallback argv=%q, want retained request %q", got, want)
		}
	}
	request := "invalid\x00selector"
	var output bytes.Buffer
	PrintManifestCommand(&output, "next: run ", []string{"lock"}, "/resolved/daem.toml", HumanOptions{ManifestRequest: &request})
	if output.Len() != 0 {
		t.Fatalf("unrepresentable retained selector emitted a command: %q", &output)
	}
}

func manifestCommandArgv(t *testing.T, command string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX concrete command contract")
	}
	output, err := exec.Command("/bin/sh", "-c", `daem() { printf '%s\000' "$@"; }; `+command).Output()
	if err != nil {
		t.Fatalf("parse printed command %q: %v", command, err)
	}
	return strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
}
