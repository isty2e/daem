package cli_test

import (
	"fmt"
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

func TestAgentConfigRemoveGuidanceSelectsLiteralID(t *testing.T) {
	const manifestContent = `version = 1
targets = ['codex']
agent_config = [{id = %q, path = 'config.toml', source = 'prefs.toml'}]
`
	for _, id := range []string{"--help", "agent-config"} {
		t.Run(id, func(t *testing.T) {
			root := t.TempDir()
			manifest := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(manifestContent, id))
			exit, stdout, stderr := testkit.RunCapturedCLI("remove", "agent-config", "--manifest", manifest, "--scope", "global", "--json", "--", id)
			if exit != 1 || stderr != "" {
				t.Fatalf("mismatched scope: %d %s %s", exit, stdout, stderr)
			}
			result := clijson.DecodeFailure(t, stdout, "remove agent-config", "operation")
			for _, guidance := range result.Guidance {
				if len(guidance.Argv) < 3 || !slices.Equal(guidance.Argv[:3], []string{"daem", "remove", "agent-config"}) {
					continue
				}
				exit, stdout, stderr := testkit.RunCapturedCLI(guidance.Argv[1:]...)
				if exit != 0 {
					t.Fatalf("suggested remove failed: %d %s %s", exit, stdout, stderr)
				}
				exit, stdout, stderr = testkit.RunCapturedCLI("remove", "agent-config", "--manifest", manifest, "--json", "--", id)
				if exit != 1 || stderr != "" {
					t.Fatalf("repeated removal should fail after following advice: %d %s %s", exit, stdout, stderr)
				}
				clijson.DecodeFailure(t, stdout, "remove agent-config", "operation")
				return
			}
			t.Fatalf("no runnable selection in %+v", result.Guidance)
		})
	}
}

func TestAgentConfigAddValidatesPlacementAndSourceFlagShape(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	const original = "version = 1\ntargets = ['codex']\n"
	testkit.WriteFile(t, root, "daem.toml", original)
	for _, example := range []struct {
		name    string
		options []string
	}{
		{"no implicit placement", []string{"--config-path", "config.toml"}},
		{"Git path does not choose host path", []string{"--path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"explicit empty Git flag", []string{"--ref=", "--config-path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"blank host path", []string{"--config-path", "  ", "--target", "codex", "--scope", "project"}},
		{"explicit empty ownership", []string{"--ownership=", "--config-path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"explicit blank syntax", []string{"--syntax", "  ", "--config-path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"multiple distinct targets", []string{"--config-path", "config.toml", "--target", "codex", "--scope", "project", "--target", "pi"}},
	} {
		t.Run(example.name, func(t *testing.T) {
			args := append([]string{"add", "agent-config", "sample", "./prefs.toml", "--manifest", manifest, "--json"}, example.options...)
			exit, stdout, stderr := testkit.RunCapturedCLI(args...)
			if exit != 2 || stderr != "" {
				t.Fatalf("invalid request not refused at ingress: %d %s %s", exit, stdout, stderr)
			}
			clijson.DecodeFailure(t, stdout, "add agent-config", "usage")
			testkit.AssertFileContent(t, manifest, original)
		})
	}
}

func TestAgentConfigAddLocksExactS3File(t *testing.T) {
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
	exit, stdout, stderr := testkit.RunCapturedCLI("add", "agent-config", "prefs", "s3://"+bucket+"/prefs.toml",
		"--manifest", manifest, "--target", "codex", "--scope", "project", "--config-path", "config.toml",
		"--region", "us-east-1", "--version-id", version, "--json")
	if exit != 0 || stderr != "" {
		t.Fatalf("add S3 host configuration failed: %d %s %s", exit, stdout, stderr)
	}
	locked, err := lockfile.Load(t.Context(), filepath.Join(root, "daem.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindAgentConfig, "prefs")
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
