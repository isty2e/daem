package platformsupport

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIMatricesMatchPlatformAdmissionRows(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/ci.yml")
	native := workflowPairs(t, workflow, "native")
	vulnerability := workflowPairs(t, workflow, "vulnerability")
	compileOnly := workflowPairs(t, workflow, "compile_only")
	releaseArtifacts := workflowPairs(t, readRepositoryFile(t, ".github/workflows/release-artifact.yml"), "artifact")

	var wantNative []string
	var wantCompileOnly []string
	for _, admission := range admissionRows {
		switch admission.Verification() {
		case VerificationNativeRequired:
			wantNative = append(wantNative, admission.Target().String())
		case VerificationCompileOnly:
			wantCompileOnly = append(wantCompileOnly, admission.Target().String())
		}
	}
	assertSameStringSet(t, "native CI rows", native, wantNative)
	assertSameStringSet(t, "vulnerability CI rows", vulnerability, wantNative)
	assertSameStringSet(t, "compile-only CI rows", compileOnly, wantCompileOnly)
	assertSameStringSet(t, "release artifact rows", releaseArtifacts, wantNative)
}

func TestReleaseArtifactWorkflowIsNonpublishingAndFailClosed(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/release-artifact.yml")
	for _, required := range []string{
		"workflow_dispatch:",
		"contents: read",
		"ref: refs/tags/${{ inputs.tag }}",
		"persist-credentials: false",
		"git show-ref --verify --quiet \"refs/tags/${RELEASE_TAG}\"",
		"go test -mod=readonly ./... -count=1",
		"-buildvcs=true",
		"CGO_ENABLED: \"0\"",
		"GOAMD64: v1",
		"GOARM64: v8.0",
		"GOFIPS140: \"off\"",
		"GOCACHE=\"${RUNNER_TEMP}/build-cache-${attempt}\"",
		"cmp \"${RUNNER_TEMP}/build-1/daem\" \"${RUNNER_TEMP}/build-2/daem\"",
		"version --json",
		"internal/releaseartifact/cmd/releasepack",
		"shasum -a 256 -c",
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02",
		"if-no-files-found: error",
		"compression-level: 0",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release artifact workflow is missing contract fragment %q", required)
		}
	}
	for _, forbidden := range []string{
		"gh release",
		"actions/create-release",
		"softprops/action-gh-release",
		"contents: write",
		"write-all",
		"continue-on-error:",
		"if: always()",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("release artifact workflow contains publishing fragment %q", forbidden)
		}
	}
}

func TestReleaseArtifactWorkflowHasOneFailClosedNativeJob(t *testing.T) {
	var workflow struct {
		On          map[string]yaml.Node   `yaml:"on"`
		Permissions map[string]string      `yaml:"permissions"`
		Jobs        map[string]workflowJob `yaml:"jobs"`
	}
	content := readRepositoryFile(t, ".github/workflows/release-artifact.yml")
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		t.Fatalf("decode release artifact workflow: %v", err)
	}

	if len(workflow.On) != 1 {
		t.Fatalf("release workflow triggers = %#v, want workflow_dispatch only", workflow.On)
	}
	dispatch, ok := workflow.On["workflow_dispatch"]
	if !ok {
		t.Fatalf("release workflow lacks workflow_dispatch trigger: %#v", workflow.On)
	}
	var dispatchConfig struct {
		Inputs map[string]struct {
			Required bool   `yaml:"required"`
			Type     string `yaml:"type"`
		} `yaml:"inputs"`
	}
	if err := dispatch.Decode(&dispatchConfig); err != nil {
		t.Fatalf("decode workflow_dispatch: %v", err)
	}
	if len(dispatchConfig.Inputs) != 1 || !dispatchConfig.Inputs["tag"].Required || dispatchConfig.Inputs["tag"].Type != "string" {
		t.Fatalf("workflow_dispatch inputs = %#v, want one required string tag", dispatchConfig.Inputs)
	}

	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Fatalf("release workflow permissions = %#v, want contents: read only", workflow.Permissions)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("release workflow jobs = %#v, want artifact only", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	job, ok := workflow.Jobs["artifact"]
	if !ok {
		t.Fatalf("release workflow lacks artifact job: %#v", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	if job.RunsOn != "${{ matrix.os }}" {
		t.Fatalf("artifact runs-on = %q, want matrix native runner", job.RunsOn)
	}
	if job.TimeoutMinutes <= 0 {
		t.Fatalf("artifact timeout-minutes = %d, want a finite positive timeout", job.TimeoutMinutes)
	}
	if job.Strategy.FailFast == nil || *job.Strategy.FailFast {
		t.Fatalf("artifact fail-fast = %v, want explicit false", job.Strategy.FailFast)
	}

	rows := make([]string, 0, len(job.Strategy.Matrix.Include))
	for _, row := range job.Strategy.Matrix.Include {
		rows = append(rows, row.Name+"|"+row.OS+"|"+row.GOOS+"/"+row.GOARCH)
	}
	assertSameStringSet(t, "release artifact native runners", rows, []string{
		"linux-amd64|ubuntu-24.04|linux/amd64",
		"darwin-arm64|macos-26|darwin/arm64",
	})

	if len(job.Steps) == 0 {
		t.Fatal("artifact job has no steps")
	}
	for _, step := range job.Steps {
		if step.ContinueOnError != nil {
			t.Fatalf("artifact step %q weakens failure with continue-on-error", step.Name)
		}
	}
	upload := job.Steps[len(job.Steps)-1]
	if upload.Uses != "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02" {
		t.Fatalf("last artifact step uses %q, want pinned private upload", upload.Uses)
	}
	if upload.If != "" {
		t.Fatalf("artifact upload has custom if %q, want default prior-step success gate", upload.If)
	}
}

type workflowJob struct {
	RunsOn         string `yaml:"runs-on"`
	TimeoutMinutes int    `yaml:"timeout-minutes"`
	Strategy       struct {
		FailFast *bool `yaml:"fail-fast"`
		Matrix   struct {
			Include []struct {
				Name   string `yaml:"name"`
				OS     string `yaml:"os"`
				GOOS   string `yaml:"goos"`
				GOARCH string `yaml:"goarch"`
			} `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []struct {
		Name            string     `yaml:"name"`
		Uses            string     `yaml:"uses"`
		If              string     `yaml:"if"`
		ContinueOnError *yaml.Node `yaml:"continue-on-error"`
	} `yaml:"steps"`
}

func TestPlatformContractsMatchCanonicalRows(t *testing.T) {
	public := readRepositoryFile(t, "docs/platforms.md")

	for _, admission := range admissionRows {
		publicRow := "| " + publicOSName(admission.Target()) + " | `" + admission.Target().Arch() + "` | " + publicSupportName(admission.Support()) + " | " + publicVerificationName(admission.Verification()) + " |"
		if !strings.Contains(public, publicRow) {
			t.Errorf("public platform matrix is missing %q", publicRow)
		}
	}
	if required := "| Every other target | any | not admitted | unverified |"; !strings.Contains(public, required) {
		t.Errorf("public platform matrix is missing fallback row %q", required)
	}
}

func TestActiveUserDocumentsPointToPlatformAuthority(t *testing.T) {
	references := map[string]string{
		"README.md":               "docs/platforms.md",
		"docs/README.md":          "platforms.md",
		"docs/getting-started.md": "platforms.md",
		"docs/cli.md":             "platforms.md",
		"docs/concepts.md":        "platforms.md",
		"docs/features.md":        "platforms.md",
	}
	for path, reference := range references {
		if content := readRepositoryFile(t, path); !strings.Contains(content, reference) {
			t.Errorf("%s does not reference %s", path, reference)
		}
	}
}

func TestActiveUserDocumentsDoNotDuplicateCanonicalPlatformRows(t *testing.T) {
	for _, path := range []string{
		"README.md",
		"docs/README.md",
		"docs/getting-started.md",
		"docs/cli.md",
		"docs/compatibility.md",
		"docs/concepts.md",
		"docs/features.md",
		"docs/manifest.md",
	} {
		content := readRepositoryFile(t, path)
		for _, rowIdentity := range []string{
			"darwin/arm64",
			"linux/amd64",
			"macOS arm64",
			"Linux amd64",
		} {
			if strings.Contains(content, rowIdentity) {
				t.Errorf("%s duplicates canonical platform row %q", path, rowIdentity)
			}
		}
	}
}

func workflowPairs(t *testing.T, content string, jobName string) []string {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						GOOS   string `yaml:"goos"`
						GOARCH string `yaml:"goarch"`
					} `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		t.Fatalf("decode CI workflow: %v", err)
	}
	job, ok := workflow.Jobs[jobName]
	if !ok {
		t.Fatalf("CI workflow job %q is missing", jobName)
	}
	if len(job.Strategy.Matrix.Include) == 0 {
		t.Fatalf("CI workflow job %q has no matrix rows", jobName)
	}
	pairs := make([]string, 0, len(job.Strategy.Matrix.Include))
	for index, row := range job.Strategy.Matrix.Include {
		if row.GOOS == "" || row.GOARCH == "" {
			t.Fatalf("CI workflow job %q row %d lacks goos/goarch", jobName, index)
		}
		pairs = append(pairs, row.GOOS+"/"+row.GOARCH)
	}
	return pairs
}

func assertSameStringSet(t *testing.T, name string, got []string, want []string) {
	t.Helper()
	gotSet := make(map[string]struct{}, len(got))
	for _, value := range got {
		if _, exists := gotSet[value]; exists {
			t.Fatalf("%s contain duplicate %q: %#v", name, value, got)
		}
		gotSet[value] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, value := range want {
		wantSet[value] = struct{}{}
	}
	if !reflect.DeepEqual(gotSet, wantSet) {
		t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

func publicOSName(target Target) string {
	switch target.OS() {
	case "darwin":
		if target.Arch() == "arm64" {
			return "macOS 26 or newer (`darwin`)"
		}
		return "macOS (`darwin`)"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return target.OS()
	}
}

func publicSupportName(support Support) string {
	if support == SupportAdmitted {
		return "admitted"
	}
	return "not admitted"
}

func publicVerificationName(verification Verification) string {
	switch verification {
	case VerificationNativeRequired:
		return "native required"
	case VerificationCompileOnly:
		return "compile only"
	default:
		return "unverified"
	}
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
