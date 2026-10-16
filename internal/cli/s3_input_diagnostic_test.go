package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunS3URIRejectionsOmitPayload(t *testing.T) {
	for _, input := range []struct {
		name   string
		values [2]string
	}{
		{name: "format-split query key", values: [2]string{"s3://private-bucket/object?to\u200bken=opaque-one", "s3://private-bucket/object?to\u200bken=opaque-two"}},
		{name: "literal escape query key", values: [2]string{`s3://private-bucket/object?to\u200bken=opaque-one`, `s3://private-bucket/object?to\u200bken=opaque-two`}},
		{name: "arbitrary query key", values: [2]string{"s3://private-bucket/object?custom-option=opaque-one", "s3://private-bucket/object?custom-option=opaque-two"}},
		{name: "userinfo", values: [2]string{"s3://user:opaque-one@private-bucket/object", "s3://user:opaque-two@private-bucket/object"}},
		{name: "syntax", values: [2]string{"s3://user:opaque-one@private-bucket/object%zz", "s3://user:opaque-two@private-bucket/object%yy"}},
	} {
		for _, commandCase := range []struct {
			args []string
			json bool
		}{
			{[]string{"apply", "--dry-run"}, false},
			{[]string{"apply", "--yes", "--verbose"}, false},
			{[]string{"apply", "--dry-run", "--json"}, true},
			{[]string{"apply", "--yes", "--json"}, true},
			{[]string{"status", "--verbose"}, false},
			{[]string{"lock", "--dry-run", "--json"}, true},
		} {
			command := commandCase.args
			t.Run(input.name+"/"+strings.Join(command, " "), func(t *testing.T) {
				root := isolatedApplyAuthorityRoot(t)
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				manifestPath := filepath.Join(root, "daem.toml")
				args := append(append([]string{}, command...), "--manifest", manifestPath)
				var first string

				for index, uri := range input.values {
					manifest := fmt.Sprintf("version=1\ntargets=[\"codex\"]\n[instructions.project]\nsource={s3=%q,version_id=\"version\",region=\"us-east-1\"}\n", uri)
					if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
						t.Fatal(err)
					}
					before := snapshotApplyInputFiles(t, root)
					var stdout, stderr bytes.Buffer
					if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
						t.Fatalf("S3 refusal code=%d stdout bytes=%d", code, stdout.Len())
					}

					message := refusalDiagnosticText(t, command[0], &stdout, &stderr, commandCase.json)
					for _, payload := range []string{"private-bucket", "opaque-one", "opaque-two"} {
						if strings.Contains(message, payload) {
							t.Error("S3 refusal echoes rejected URI data")
						}
					}
					if !strings.Contains(message, "instructions.project.source.s3") {
						t.Error("S3 refusal lost the owning manifest field")
					}
					if index == 0 {
						first = message
					} else if message != first {
						t.Error("human diagnostic depends on a rejected URI value")
					}
					if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
						t.Error("S3 input refusal changed the workspace")
					}
				}
			})
		}
	}
}
