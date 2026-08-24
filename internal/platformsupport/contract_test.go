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
	releaseWorkflow := readRepositoryFile(t, ".github/workflows/release-artifact.yml")
	releaseArtifacts := workflowPairs(t, releaseWorkflow, "artifact")
	releaseScans := workflowPairs(t, releaseWorkflow, "vulnerability-scan")

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
	assertSameStringSet(t, "release vulnerability-scan rows", releaseScans, wantNative)
}

func TestReleaseArtifactWorkflowIsNonpublishingAndFailClosed(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/release-artifact.yml")
	for _, required := range []string{
		"workflow_dispatch:",
		"contents: read",
		"ref: refs/tags/${{ inputs.tag }}",
		"persist-credentials: false",
		"git show-ref --verify --quiet \"refs/tags/${RELEASE_TAG}\"",
		"tools/test.sh full",
		"-buildvcs=true",
		"CGO_ENABLED: \"0\"",
		"GOAMD64: v1",
		"GOARM64: v8.0",
		"GOFIPS140: \"off\"",
		"GOCACHE=\"${RUNNER_TEMP}/build-cache-${attempt}\"",
		"cmp \"${RUNNER_TEMP}/build-1/daem\" \"${RUNNER_TEMP}/build-2/daem\"",
		"version --json",
		"assert_installer_requirement DAEM_VERSION \"${RELEASE_TAG}\"",
		"assert_installer_requirement_if_present DAEM_REVISION_TIME \"${revision_time}\"",
		"assert_installer_requirement_if_present DAEM_GO_VERSION \"${RELEASE_GO_VERSION}\"",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0",
		"-C cmd/daem -scan=module -show verbose",
		"-test -scan=package -show verbose",
		"-test -scan=symbol -show verbose",
		"exit \"${scan_status}\"",
		"needs: vulnerability-scan",
		"Exercise documented installer flow against local artifacts",
		"python3 -m http.server \"${installer_port}\" --bind 127.0.0.1",
		"grep -F 'curl --fail --location' \"${recipe}\"",
		"downloaded archive does not match its exact checksum entry",
		"downloaded daem binary does not match the requested release identity",
		"tar -czf \"${alien_dir}/${archive_name}\" -C \"${alien_stage}\" daem",
		"internal/releaseartifact/cmd/releasepack",
		"shasum -a 256 -c",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
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
		"DAEM_REVISION=0000000000000000000000000000000000000000",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("release artifact workflow contains publishing fragment %q", forbidden)
		}
	}
}

func TestReleaseArtifactWorkflowIsolatesScanningBeforeTagControlledCode(t *testing.T) {
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
	if len(workflow.Jobs) != 2 {
		t.Fatalf("release workflow jobs = %#v, want vulnerability-scan and artifact", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	scanJob, ok := workflow.Jobs["vulnerability-scan"]
	if !ok {
		t.Fatalf("release workflow lacks vulnerability-scan job: %#v", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	job, ok := workflow.Jobs["artifact"]
	if !ok {
		t.Fatalf("release workflow lacks artifact job: %#v", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	if !reflect.DeepEqual(releaseJobNeeds(t, job), []string{"vulnerability-scan"}) {
		t.Fatalf("artifact needs = %#v, want dependency on vulnerability-scan so scanning precedes tag-controlled code", releaseJobNeeds(t, job))
	}

	assertReleaseJobShape(t, "vulnerability-scan", scanJob)
	assertReleaseJobShape(t, "artifact", job)

	wantScanSteps := []string{
		"Check out selected tag",
		"Set up exact default Go toolchain",
		"Validate tag, toolchain, and native target",
		"Scan exact tag for known vulnerabilities",
	}
	assertReleaseStepSequence(t, "vulnerability-scan", scanJob, wantScanSteps)
	scan := scanJob.Steps[len(scanJob.Steps)-1]
	for _, required := range []string{
		"set -euo pipefail",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 \\\n  -C cmd/daem -scan=module -show verbose || scan_status=1",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 \\\n  -test -scan=package -show verbose ./... || scan_status=1",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 \\\n  -test -scan=symbol -show verbose ./... || scan_status=1",
		"exit \"${scan_status}\"",
	} {
		if !strings.Contains(scan.Run, required) {
			t.Errorf("vulnerability scan step is missing control-flow fragment %q", required)
		}
	}
	if strings.Contains(scan.Run, "|| true") {
		t.Error("vulnerability scan step swallows failures with || true")
	}
	for _, step := range scanJob.Steps {
		if strings.Contains(step.Run, "tools/test.sh") {
			t.Errorf("vulnerability-scan step %q runs tag-controlled scripts before scanning", step.Name)
		}
	}

	wantArtifactSteps := []string{
		"Check out selected tag",
		"Set up exact default Go toolchain",
		"Validate tag, revision, toolchain, and native target",
		"Run full native tests before artifact construction",
		"Build twice from frozen inputs",
		"Verify embedded version contract",
		"Smoke-test native CLI artifact",
		"Assemble and verify archive twice",
		"Build development binary for identity rehearsals",
		"Exercise documented installer flow against local artifacts",
		"Smoke-test install, source upgrade, and executable rollback",
		"Upload private workflow artifact",
	}
	assertReleaseStepSequence(t, "artifact", job, wantArtifactSteps)

	upload := job.Steps[len(job.Steps)-1]
	if upload.Uses != "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a" {
		t.Fatalf("last artifact step uses %q, want pinned private upload", upload.Uses)
	}
	if upload.If != "" {
		t.Fatalf("artifact upload has custom if %q, want default prior-step success gate", upload.If)
	}
}

func assertReleaseJobShape(t *testing.T, name string, job workflowJob) {
	t.Helper()
	if job.RunsOn != "${{ matrix.os }}" {
		t.Fatalf("%s runs-on = %q, want matrix native runner", name, job.RunsOn)
	}
	if job.TimeoutMinutes <= 0 {
		t.Fatalf("%s timeout-minutes = %d, want a finite positive timeout", name, job.TimeoutMinutes)
	}
	if job.Strategy.FailFast == nil || *job.Strategy.FailFast {
		t.Fatalf("%s fail-fast = %v, want explicit false", name, job.Strategy.FailFast)
	}

	rows := make([]string, 0, len(job.Strategy.Matrix.Include))
	for _, row := range job.Strategy.Matrix.Include {
		rows = append(rows, row.Name+"|"+row.OS+"|"+row.GOOS+"/"+row.GOARCH)
	}
	assertSameStringSet(t, name+" native runners", rows, []string{
		"linux-amd64|ubuntu-24.04|linux/amd64",
		"darwin-arm64|macos-26|darwin/arm64",
	})

	if len(job.Steps) == 0 {
		t.Fatalf("%s job has no steps", name)
	}
	for _, step := range job.Steps {
		if step.ContinueOnError != nil {
			t.Fatalf("%s step %q weakens failure with continue-on-error", name, step.Name)
		}
	}
}

func assertReleaseStepSequence(t *testing.T, jobName string, job workflowJob, want []string) {
	t.Helper()
	names := make([]string, 0, len(job.Steps))
	for _, step := range job.Steps {
		names = append(names, step.Name)
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("%s step sequence = %#v, want %#v", jobName, names, want)
	}
}

func releaseJobNeeds(t *testing.T, job workflowJob) []string {
	t.Helper()
	switch job.Needs.Kind {
	case 0:
		return nil
	case yaml.ScalarNode:
		return []string{job.Needs.Value}
	case yaml.SequenceNode:
		var needs []string
		if err := job.Needs.Decode(&needs); err != nil {
			t.Fatalf("decode job needs: %v", err)
		}
		return needs
	default:
		t.Fatalf("job needs has unexpected YAML kind %v", job.Needs.Kind)
		return nil
	}
}

type workflowJob struct {
	RunsOn         string    `yaml:"runs-on"`
	Needs          yaml.Node `yaml:"needs"`
	TimeoutMinutes int       `yaml:"timeout-minutes"`
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
		Run             string     `yaml:"run"`
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

func TestDarwinPathDocumentationSeparatesProvisionalAndExactAuthority(t *testing.T) {
	public := strings.Join(strings.Fields(readRepositoryFile(t, "docs/platforms.md")), " ")
	for _, required := range []string{
		"provisional comparison and exclusion evidence",
		"does not grant exact path authority",
		"fails before effects if the captured root directory is replaced",
		"destination crosses onto a different descendant mount",
	} {
		if !strings.Contains(public, required) {
			t.Errorf("platform documentation is missing Darwin path-authority contract %q", required)
		}
	}
	if strings.Contains(public, "same-batch provisional alias races remain outside") {
		t.Error("platform documentation retains the superseded provisional-authority exclusion")
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
