package manifest

import (
	"fmt"
	"path/filepath"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestHostConfigPlacementFollowsSelectedManifest(t *testing.T) {
	for _, host := range []string{"pi", "codex"} {
		for _, scope := range []string{"global", "project"} {
			for _, origin := range []daempaths.ManifestOrigin{
				daempaths.ManifestOriginUserDefault,
				daempaths.ManifestOriginCWD,
				daempaths.ManifestOriginExplicit,
			} {
				t.Run(host+"/"+scope+"/"+string(origin), func(t *testing.T) {
					root := t.TempDir()
					paths := daempaths.Paths{
						ManifestPath: filepath.Join(root, "daem.toml"), ManifestRoot: root,
						ManifestOrigin: origin,
					}
					environment, err := Decode([]byte(fmt.Sprintf(`version = 1
targets = [%q]
[[host_config]]
id = "settings"
scope = %q
path = "extension/settings.json"
source = %q
`, host, scope, filepath.Join(root, "settings.json"))))
					if err != nil {
						t.Fatal(err)
					}
					err = ValidatePlacement(paths, environment)
					refused := origin == daempaths.ManifestOriginUserDefault && scope == "project"
					if (err != nil) != refused {
						t.Fatalf("placement refusal = %v, want %v", err, refused)
					}
				})
			}
		}
	}
}
