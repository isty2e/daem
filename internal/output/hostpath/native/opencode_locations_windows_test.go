package native

import (
	"path/filepath"
	"testing"
)

func TestOpenCodeRootedSelectorsUseTheCapturedVolume(t *testing.T) {
	for _, work := range []string{`C:\native-context`, `\\server\share\native-context`} {
		for _, value := range []string{`\x.db`, `/x.db`} {
			for _, row := range []struct{ variable, identity, joined string }{
				{"OPENCODE_DB", "opencode-database-override", value},
				{"OPENCODE_PLUGIN_META_FILE", "opencode-plugin-metadata-override", value},
				{"OPENCODE_CONFIG_DIR", "opencode-additional-configuration", filepath.Clean(value)},
				{"XDG_DATA_HOME", "opencode-data", filepath.Join(value, "opencode")},
			} {
				locations, err := NewLocations(work, `C:\native-home`, map[string]string{row.variable: value})
				if err != nil {
					t.Fatal(err)
				}
				location, err := locations.ResolveProtection(protectionReference(t, row.identity))
				want := filepath.VolumeName(work) + row.joined
				if err != nil || location.Path() != want || !filepath.IsAbs(location.Path()) {
					t.Fatal("drive-less rooted selector became context/data-relative", row.identity, location, err, want)
				}
			}
		}
	}
}
