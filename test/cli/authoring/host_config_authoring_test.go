package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestHostConfigRemoveGuidanceSelectsLiteralID(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	const original = `version = 1
targets = ['codex']
host_config = [{id = '--help', path = 'config.toml', source = 'prefs.toml'}]
`
	testkit.WriteFile(t, root, "daem.toml", original)
	exit, stdout, stderr := testkit.RunCapturedCLI("remove", "host-config", "--manifest", manifest, "--scope", "global", "--json", "--", "--help")
	if exit != 1 || stderr != "" {
		t.Fatalf("mismatched scope: %d %s %s", exit, stdout, stderr)
	}
	result := clijson.DecodeFailure(t, stdout, "remove host-config", "operation")
	for _, guidance := range result.Guidance {
		if len(guidance.Argv) >= 3 && slices.Equal(guidance.Argv[:3], []string{"daem", "remove", "host-config"}) {
			exit, stdout, stderr := testkit.RunCapturedCLI(guidance.Argv[1:]...)
			if exit != 0 {
				t.Fatalf("suggested remove failed: %d %s %s", exit, stdout, stderr)
			}
			exit, stdout, stderr = testkit.RunCapturedCLI("remove", "host-config", "--manifest", manifest, "--json", "--", "--help")
			if exit != 1 || stderr != "" {
				t.Fatalf("repeated removal should fail after following advice: %d %s %s", exit, stdout, stderr)
			}
			clijson.DecodeFailure(t, stdout, "remove host-config", "operation")
			return
		}
	}
	t.Fatalf("no runnable selection in %+v", result.Guidance)
}

func TestHostConfigAddValidatesPlacementAndSourceFlagShape(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	const original = "version = 1\ntargets = ['codex']\n"
	testkit.WriteFile(t, root, "daem.toml", original)
	for _, example := range []struct {
		name    string
		options []string
	}{
		{"no implicit placement", []string{"--host-path", "config.toml"}},
		{"Git path does not choose host path", []string{"--path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"explicit empty Git flag", []string{"--ref=", "--host-path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"multiple distinct targets", []string{"--host-path", "config.toml", "--target", "codex", "--scope", "project", "--target", "pi"}},
	} {
		t.Run(example.name, func(t *testing.T) {
			args := append([]string{"add", "host-config", "sample", "./prefs.toml", "--manifest", manifest, "--json"}, example.options...)
			exit, stdout, stderr := testkit.RunCapturedCLI(args...)
			if exit != 2 || stderr != "" {
				t.Fatalf("invalid request not refused at ingress: %d %s %s", exit, stdout, stderr)
			}
			clijson.DecodeFailure(t, stdout, "add host-config", "usage")
			testkit.AssertFileContent(t, manifest, original)
		})
	}
}

func TestHostConfigAddLocksExactS3File(t *testing.T) {
	const bucket = "native-config-fixture"
	const version = "fixture-version"
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/"+bucket+"/prefs.toml" || request.URL.Query().Get("versionId") != version {
			t.Errorf("unexpected S3 request: %s %s", request.Method, request.URL)
			out.WriteHeader(http.StatusBadRequest)
			return
		}
		out.Header().Set("x-amz-version-id", version)
		_, _ = io.WriteString(out, "fixture = 'value from the object'\n")
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	t.Setenv("AWS_ENDPOINT_URL_S3", server.URL)
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(root, "no-aws-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(root, "no-aws-credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	manifest := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = ['codex']\n")
	exit, stdout, stderr := testkit.RunCapturedCLI("add", "host-config", "prefs", "s3://"+bucket+"/prefs.toml",
		"--manifest", manifest, "--target", "codex", "--scope", "project", "--host-path", "config.toml",
		"--region", "us-east-1", "--version-id", version, "--json")
	if exit != 0 || stderr != "" {
		t.Fatalf("add S3 host configuration failed: %d %s %s", exit, stdout, stderr)
	}
	locked, err := lockfile.Load(t.Context(), filepath.Join(root, "daem.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "prefs")
	if err != nil {
		t.Fatal(err)
	}
	subject, ok := locked.Locked.ExactSupplySubject(id)
	identity, exact := subject.ExactSupply()
	if !ok || !exact || string(identity.ResolvedRef()) != version {
		t.Fatalf("locked source lacks the requested S3 version: %v", identity)
	}
	testkit.AssertPathMissing(t, filepath.Join(root, ".codex", "config.toml"))
}
