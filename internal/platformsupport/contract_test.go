package platformsupport

import (
	"os"
	"os/exec"
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

func TestStateBarrierCIMatrixCoversNativeMinimumAndRaceContracts(t *testing.T) {
	var workflow struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	content := readRepositoryFile(t, ".github/workflows/ci.yml")
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		t.Fatalf("decode CI workflow: %v", err)
	}
	job, ok := workflow.Jobs["state_barrier"]
	if !ok {
		t.Fatal("CI workflow lacks state_barrier job")
	}
	if job.Name != "State Barrier (${{ matrix.name }})" || job.RunsOn != "${{ matrix.os }}" {
		t.Fatalf("state_barrier identity = (%q, %q)", job.Name, job.RunsOn)
	}
	if job.Strategy.FailFast == nil || *job.Strategy.FailFast {
		t.Fatalf("state_barrier fail-fast = %v, want false", job.Strategy.FailFast)
	}

	type matrixRow struct {
		OS        string
		GOOS      string
		GOARCH    string
		GoVersion string
		Race      bool
	}
	want := map[string]matrixRow{
		"linux-amd64":            {OS: "ubuntu-24.04", GOOS: "linux", GOARCH: "amd64", GoVersion: "1.26.6"},
		"darwin-arm64":           {OS: "macos-26", GOOS: "darwin", GOARCH: "arm64", GoVersion: "1.26.6"},
		"windows-amd64":          {OS: "windows-2025", GOOS: "windows", GOARCH: "amd64", GoVersion: "1.26.6"},
		"minimum-go-linux-amd64": {OS: "ubuntu-24.04", GOOS: "linux", GOARCH: "amd64", GoVersion: "1.25.12"},
		"race-linux-amd64":       {OS: "ubuntu-24.04", GOOS: "linux", GOARCH: "amd64", GoVersion: "1.26.6", Race: true},
	}
	if len(job.Strategy.Matrix.Include) != len(want) {
		t.Fatalf("state_barrier matrix rows = %d, want %d", len(job.Strategy.Matrix.Include), len(want))
	}
	for _, row := range job.Strategy.Matrix.Include {
		expected, present := want[row.Name]
		if !present {
			t.Fatalf("state_barrier has unexpected row %q", row.Name)
		}
		got := matrixRow{
			OS:        row.OS,
			GOOS:      row.GOOS,
			GOARCH:    row.GOARCH,
			GoVersion: row.GoVersion,
			Race:      row.Race,
		}
		if got != expected {
			t.Fatalf("state_barrier row %q = %#v, want %#v", row.Name, got, expected)
		}
		delete(want, row.Name)
	}

	var setupStep, contractStep *releaseStep
	for index := range job.Steps {
		step := &job.Steps[index]
		switch step.Name {
		case "Set up selected Go toolchain":
			setupStep = step
		case "Run named persistence and recovery contracts":
			contractStep = step
		}
	}
	if setupStep == nil || setupStep.Uses != "actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c" ||
		setupStep.With["go-version"].Value != "${{ matrix.goversion }}" {
		t.Fatalf("state_barrier setup step = %#v", setupStep)
	}
	if contractStep == nil || contractStep.Shell != "bash" || contractStep.ContinueOnError != nil ||
		!strings.Contains(contractStep.Run, "tools/test-state-barrier.sh") ||
		!strings.Contains(contractStep.Run, "tools/test-state-barrier.sh --race") {
		t.Fatalf("state_barrier contract step = %#v", contractStep)
	}
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
		"resolve-tag:",
		"release_commit: ${{ steps.resolve.outputs.release_commit }}",
		"release_toolchain: ${{ steps.toolchain.outputs.release_toolchain }}",
		"release_setup_go_version: ${{ steps.toolchain.outputs.release_setup_go_version }}",
		"ref: ${{ needs.resolve-tag.outputs.release_commit }}",
		"RELEASE_COMMIT: ${{ needs.resolve-tag.outputs.release_commit }}",
		"RELEASE_TOOLCHAIN: ${{ needs.resolve-tag.outputs.release_toolchain }}",
		"go-version: ${{ needs.resolve-tag.outputs.release_setup_go_version }}",
		"Exercise documented installer flow against local artifacts",
		"python3 -m http.server \"${installer_port}\" --bind 127.0.0.1",
		"grep -F 'curl --fail --location' \"${recipe}\"",
		"write_release_metadata() {",
		`'DAEM_ORIGIN_API="https://api.github.com/repos/isty2e/daem"'`,
		`"DAEM_ORIGIN_API=\"http://127.0.0.1:${installer_port}\""`,
		`"${directory}/commits/refs/tags/${RELEASE_TAG}"`,
		`printf '%s' "${RELEASE_REVISION}"`,
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

func TestReleaseArtifactWorkflowBindsEveryJobToOneResolvedCommit(t *testing.T) {
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
	if len(workflow.Jobs) != 3 {
		t.Fatalf("release workflow jobs = %#v, want resolve-tag, vulnerability-scan, and artifact", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	resolveJob, ok := workflow.Jobs["resolve-tag"]
	if !ok {
		t.Fatalf("release workflow lacks resolve-tag job: %#v", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	scanJob, ok := workflow.Jobs["vulnerability-scan"]
	if !ok {
		t.Fatalf("release workflow lacks vulnerability-scan job: %#v", reflect.ValueOf(workflow.Jobs).MapKeys())
	}
	artifactJob, ok := workflow.Jobs["artifact"]
	if !ok {
		t.Fatalf("release workflow lacks artifact job: %#v", reflect.ValueOf(workflow.Jobs).MapKeys())
	}

	assertReleaseTagResolutionJob(t, resolveJob)

	if !reflect.DeepEqual(releaseJobNeeds(t, scanJob), []string{"resolve-tag"}) {
		t.Fatalf("vulnerability-scan needs = %#v, want dependency on resolve-tag", releaseJobNeeds(t, scanJob))
	}
	if !reflect.DeepEqual(releaseJobNeeds(t, artifactJob), []string{"resolve-tag", "vulnerability-scan"}) {
		t.Fatalf("artifact needs = %#v, want dependency on resolve-tag and vulnerability-scan", releaseJobNeeds(t, artifactJob))
	}

	assertReleaseJobShape(t, "vulnerability-scan", scanJob)
	assertReleaseJobShape(t, "artifact", artifactJob)

	assertReleaseStepSequence(t, "vulnerability-scan", scanJob, []string{
		"Check out resolved release commit",
		"Set up resolved release toolchain",
		"Validate commit, tag, toolchain, and native target",
		"Scan resolved commit for known vulnerabilities",
	})
	assertReleaseStepSequence(t, "artifact", artifactJob, []string{
		"Check out resolved release commit",
		"Set up resolved release toolchain",
		"Validate commit, tag, revision, toolchain, and native target",
		"Run full native tests before artifact construction",
		"Build twice from frozen inputs",
		"Verify embedded version contract",
		"Smoke-test native CLI artifact",
		"Assemble and verify archive twice",
		"Build development binary for identity rehearsals",
		"Exercise documented installer flow against local artifacts",
		"Smoke-test install, source upgrade, and executable rollback",
		"Upload private workflow artifact",
	})

	assertReleaseResolvedCheckout(t, "vulnerability-scan", scanJob.Steps[0])
	assertReleaseResolvedCheckout(t, "artifact", artifactJob.Steps[0])
	assertReleaseSetupStep(t, "vulnerability-scan", scanJob.Steps[1])
	assertReleaseSetupStep(t, "artifact", artifactJob.Steps[1])
	assertReleaseScanStep(t, scanJob.Steps[len(scanJob.Steps)-1])
	for _, step := range scanJob.Steps {
		if strings.Contains(step.Run, "tools/test.sh") {
			t.Errorf("vulnerability-scan step %q runs tag-controlled scripts before scanning", step.Name)
		}
	}

	upload := artifactJob.Steps[len(artifactJob.Steps)-1]
	if upload.Uses != "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a" {
		t.Fatalf("last artifact step uses %q, want pinned private upload", upload.Uses)
	}
	if upload.If != "" {
		t.Fatalf("artifact upload has custom if %q, want default prior-step success gate", upload.If)
	}
}

func assertReleaseTagResolutionJob(t *testing.T, job workflowJob) {
	t.Helper()
	if job.RunsOn != "ubuntu-24.04" {
		t.Fatalf("resolve-tag runs-on = %q, want fixed ubuntu-24.04", job.RunsOn)
	}
	if job.TimeoutMinutes <= 0 {
		t.Fatalf("resolve-tag timeout-minutes = %d, want a finite positive timeout", job.TimeoutMinutes)
	}
	if job.If != "" {
		t.Fatalf("resolve-tag has custom if %q", job.If)
	}
	if !reflect.DeepEqual(job.Outputs, map[string]string{
		"release_commit":           "${{ steps.resolve.outputs.release_commit }}",
		"release_setup_go_version": "${{ steps.toolchain.outputs.release_setup_go_version }}",
		"release_toolchain":        "${{ steps.toolchain.outputs.release_toolchain }}",
	}) {
		t.Fatalf("resolve-tag outputs = %#v, want commit and toolchain outputs", job.Outputs)
	}
	assertReleaseStepSequence(t, "resolve-tag", job, []string{
		"Check out selected tag",
		"Resolve selected tag to exactly one commit",
		"Resolve selected tag toolchain",
	})
	checkout := job.Steps[0]
	if checkout.Uses != "actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd" {
		t.Fatalf("resolve-tag checkout uses %q, want pinned checkout action", checkout.Uses)
	}
	if checkout.If != "" {
		t.Fatalf("resolve-tag checkout has custom if %q", checkout.If)
	}
	assertReleaseStepWith(t, "resolve-tag checkout", checkout, map[string]string{
		"ref":                 "refs/tags/${{ inputs.tag }}",
		"fetch-depth":         "0",
		"persist-credentials": "false",
	})
	for _, step := range job.Steps {
		if step.ContinueOnError != nil {
			t.Fatalf("resolve-tag step %q weakens failure with continue-on-error", step.Name)
		}
	}
	resolve := job.Steps[1]
	if resolve.Id != "resolve" {
		t.Fatalf("resolve-tag resolution step id = %q, want resolve", resolve.Id)
	}
	if resolve.If != "" {
		t.Fatalf("resolve-tag resolution step has custom if %q", resolve.If)
	}
	for _, required := range []string{
		"set -euo pipefail",
		"git check-ref-format \"refs/tags/${RELEASE_TAG}\"",
		"git show-ref --verify --quiet \"refs/tags/${RELEASE_TAG}\"",
		"git rev-parse --verify \"refs/tags/${RELEASE_TAG}^{commit}\"",
		"grep -E -x '[0-9a-f]{40}'",
		"test \"$(git rev-parse --verify HEAD)\" = \"${release_commit}\"",
		"printf 'release_commit=%s\\n' \"${release_commit}\" >> \"${GITHUB_OUTPUT}\"",
	} {
		if !strings.Contains(resolve.Run, required) {
			t.Errorf("resolve-tag resolution step is missing fragment %q", required)
		}
	}

	toolchain := job.Steps[2]
	if toolchain.Id != "toolchain" {
		t.Fatalf("resolve-tag toolchain step id = %q, want toolchain", toolchain.Id)
	}
	if toolchain.If != "" {
		t.Fatalf("resolve-tag toolchain step has custom if %q", toolchain.If)
	}
	ordered := []string{
		`$1 == "toolchain"`,
		"grep -E -x 'go[0-9]+\\.[0-9]+\\.[0-9]+'",
		`/^DAEM_GO_VERSION=/`,
		`test "${documented_toolchain}" = "${release_toolchain}"`,
		`setup_go_version="${release_toolchain#go}"`,
		"grep -E -x '[0-9]+\\.[0-9]+\\.[0-9]+'",
		"printf 'release_toolchain=%s\\n' \"${release_toolchain}\" >> \"${GITHUB_OUTPUT}\"",
		"printf 'release_setup_go_version=%s\\n' \"${setup_go_version}\" >> \"${GITHUB_OUTPUT}\"",
	}
	last := -1
	for _, fragment := range ordered {
		index := strings.Index(toolchain.Run, fragment)
		if index < 0 {
			t.Fatalf("resolve-tag toolchain step is missing fragment %q", fragment)
		}
		if index < last {
			t.Fatalf("resolve-tag toolchain fragment %q is out of order", fragment)
		}
		last = index
	}
}

func assertReleaseResolvedCheckout(t *testing.T, jobName string, step releaseStep) {
	t.Helper()
	if step.Uses != "actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd" {
		t.Fatalf("%s checkout uses %q, want pinned checkout action", jobName, step.Uses)
	}
	if step.If != "" {
		t.Fatalf("%s checkout has custom if %q", jobName, step.If)
	}
	assertReleaseStepWith(t, jobName+" checkout", step, map[string]string{
		"ref":                 "${{ needs.resolve-tag.outputs.release_commit }}",
		"fetch-depth":         "0",
		"persist-credentials": "false",
	})
}

func assertReleaseSetupStep(t *testing.T, jobName string, setup releaseStep) {
	t.Helper()
	if setup.Uses != "actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c" {
		t.Fatalf("%s setup-go uses %q, want pinned setup-go action", jobName, setup.Uses)
	}
	if setup.If != "" {
		t.Fatalf("%s setup-go has custom if %q", jobName, setup.If)
	}
	assertReleaseStepWith(t, jobName+" setup-go", setup, map[string]string{
		"go-version":            "${{ needs.resolve-tag.outputs.release_setup_go_version }}",
		"cache-dependency-path": "go.sum",
	})
}

func TestReleaseArtifactToolchainResolutionContract(t *testing.T) {
	var workflow struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	content := readRepositoryFile(t, ".github/workflows/release-artifact.yml")
	if err := yaml.Unmarshal([]byte(content), &workflow); err != nil {
		t.Fatalf("decode release artifact workflow: %v", err)
	}
	resolveJob, ok := workflow.Jobs["resolve-tag"]
	if !ok {
		t.Fatal("release workflow lacks resolve-tag job")
	}
	var toolchain releaseStep
	for _, step := range resolveJob.Steps {
		if step.Name == "Resolve selected tag toolchain" {
			toolchain = step
			break
		}
	}
	if toolchain.Run == "" {
		t.Fatal("release workflow lacks executable selected-tag toolchain resolution")
	}

	runToolchain := func(t *testing.T, directory string) (string, error) {
		t.Helper()
		outputPath := filepath.Join(t.TempDir(), "github-output")
		command := exec.Command("bash", "-c", toolchain.Run)
		command.Dir = directory
		command.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath)
		combined, err := command.CombinedOutput()
		if err != nil {
			return string(combined), err
		}
		output, readErr := os.ReadFile(outputPath)
		if readErr != nil {
			return "", readErr
		}
		return string(output), nil
	}

	const expected = "release_toolchain=go1.26.5\nrelease_setup_go_version=1.26.5\n"
	fixtureDirectory := filepath.Join("testdata", "release", "v0.1.0")
	goMod, err := os.ReadFile(filepath.Join(fixtureDirectory, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	historicalInstall, err := os.ReadFile(filepath.Join(fixtureDirectory, "install.md.fixture"))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("historical v0.1.0 without documented toolchain", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "go.mod"), goMod, 0o600); err != nil {
			t.Fatal(err)
		}
		docsDirectory := filepath.Join(directory, "docs")
		if err := os.MkdirAll(docsDirectory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(docsDirectory, "install.md"), historicalInstall, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := runToolchain(t, directory)
		if err != nil {
			t.Fatalf("resolve historical v0.1.0 toolchain: %v\n%s", err, output)
		}
		if output != expected {
			t.Fatalf("historical v0.1.0 toolchain output = %q, want %q", output, expected)
		}
	})

	for _, test := range []struct {
		name      string
		directive string
		accepted  bool
	}{
		{name: "inline toolchain comment", directive: "toolchain go1.26.5 // release compiler", accepted: true},
		{name: "adjacent inline toolchain comment", directive: "toolchain go1.26.5//release compiler", accepted: true},
		{name: "unexpected trailing token", directive: "toolchain go1.26.5 release-compiler"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			modifiedGoMod := strings.Replace(string(goMod), "toolchain go1.26.5", test.directive, 1)
			if modifiedGoMod == string(goMod) {
				t.Fatal("historical fixture lacks the expected toolchain directive")
			}
			if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(modifiedGoMod), 0o600); err != nil {
				t.Fatal(err)
			}
			docsDirectory := filepath.Join(directory, "docs")
			if err := os.MkdirAll(docsDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(docsDirectory, "install.md"), historicalInstall, 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := runToolchain(t, directory)
			if (err == nil) != test.accepted {
				t.Fatalf("toolchain resolution error = %v, want accepted=%t\n%s", err, test.accepted, output)
			}
			if test.accepted && output != expected {
				t.Fatalf("toolchain output = %q, want %q", output, expected)
			}
		})
	}

	for _, test := range []struct {
		name       string
		documented string
		accepted   bool
	}{
		{name: "matching documented toolchain", documented: "DAEM_GO_VERSION=go1.26.5\n", accepted: true},
		{name: "mismatched documented toolchain", documented: "DAEM_GO_VERSION=go1.26.6\n"},
		{name: "empty documented toolchain", documented: "DAEM_GO_VERSION=\n"},
		{name: "duplicate documented toolchain", documented: "DAEM_GO_VERSION=go1.26.5\nDAEM_GO_VERSION=go1.26.5\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "go.mod"), goMod, 0o600); err != nil {
				t.Fatal(err)
			}
			docsDirectory := filepath.Join(directory, "docs")
			if err := os.MkdirAll(docsDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(docsDirectory, "install.md"), []byte(test.documented), 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := runToolchain(t, directory)
			if (err == nil) != test.accepted {
				t.Fatalf("toolchain resolution error = %v, want accepted=%t\n%s", err, test.accepted, output)
			}
			if test.accepted && output != expected {
				t.Fatalf("toolchain output = %q, want %q", output, expected)
			}
		})
	}

	t.Run("missing tag toolchain authority", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module github.com/isty2e/daem\n\ngo 1.25.0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		docsDirectory := filepath.Join(directory, "docs")
		if err := os.MkdirAll(docsDirectory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(docsDirectory, "install.md"), []byte("DAEM_VERSION=v0.1.0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := runToolchain(t, directory); err == nil {
			t.Fatalf("missing tag toolchain unexpectedly accepted\n%s", output)
		}
	})
}

func assertReleaseScanStep(t *testing.T, scan releaseStep) {
	t.Helper()
	if scan.If != "" {
		t.Fatalf("vulnerability scan step has custom if %q, want unconditional execution", scan.If)
	}
	ordered := []string{
		"set -euo pipefail",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 -version",
		"scan_status=0",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 \\\n  -C cmd/daem -scan=module -show verbose || scan_status=1",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 \\\n  -test -scan=package -show verbose ./... || scan_status=1",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 \\\n  -test -scan=symbol -show verbose ./... || scan_status=1",
		"exit \"${scan_status}\"",
	}
	last := -1
	for _, fragment := range ordered {
		index := strings.Index(scan.Run, fragment)
		if index < 0 {
			t.Fatalf("vulnerability scan step is missing control-flow fragment %q", fragment)
		}
		if index < last {
			t.Fatalf("vulnerability scan step fragment %q is out of order", fragment)
		}
		last = index
	}
	if count := strings.Count(scan.Run, "scan_status=0"); count != 1 {
		t.Fatalf("vulnerability scan step initializes scan_status %d times, want exactly one before propagation", count)
	}
	if !strings.HasSuffix(strings.TrimSpace(scan.Run), `exit "${scan_status}"`) {
		t.Fatal("vulnerability scan step does not end by propagating scan_status")
	}
	if strings.Contains(scan.Run, "|| true") {
		t.Error("vulnerability scan step swallows failures with || true")
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
	if job.If != "" {
		t.Fatalf("%s has custom if %q", name, job.If)
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

func assertReleaseStepWith(t *testing.T, stepName string, step releaseStep, want map[string]string) {
	t.Helper()
	if len(step.With) != len(want) {
		t.Fatalf("%s with = %#v, want exactly %#v", stepName, step.With, want)
	}
	for key, value := range want {
		field, ok := step.With[key]
		if !ok {
			t.Fatalf("%s lacks with.%s", stepName, key)
		}
		if field.Value != value {
			t.Fatalf("%s with.%s = %q, want %q", stepName, key, field.Value, value)
		}
	}
}

type releaseStep struct {
	Name            string               `yaml:"name"`
	Id              string               `yaml:"id"`
	Uses            string               `yaml:"uses"`
	If              string               `yaml:"if"`
	Run             string               `yaml:"run"`
	Shell           string               `yaml:"shell"`
	With            map[string]yaml.Node `yaml:"with"`
	ContinueOnError *yaml.Node           `yaml:"continue-on-error"`
}

type workflowJob struct {
	Name           string            `yaml:"name"`
	RunsOn         string            `yaml:"runs-on"`
	If             string            `yaml:"if"`
	Needs          yaml.Node         `yaml:"needs"`
	TimeoutMinutes int               `yaml:"timeout-minutes"`
	Outputs        map[string]string `yaml:"outputs"`
	Strategy       struct {
		FailFast *bool `yaml:"fail-fast"`
		Matrix   struct {
			Include []struct {
				Name      string `yaml:"name"`
				OS        string `yaml:"os"`
				GOOS      string `yaml:"goos"`
				GOARCH    string `yaml:"goarch"`
				GoVersion string `yaml:"goversion"`
				Race      bool   `yaml:"race"`
			} `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []releaseStep `yaml:"steps"`
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

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
