package install_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallerResolvesUnixUtilitiesFromPath(t *testing.T) {
	fixture := newInstallerFixture(t, "")
	adapters := filepath.Join(fixture.root, "adapters")
	utilities := []string{"sh", "tar", "gzip", "cmp", "mkdir", "mktemp", "rm", "install", "mv"}
	if runtime.GOOS == "darwin" {
		utilities = append(utilities, "shasum", "perl")
	} else {
		utilities = append(utilities, "sha256sum")
	}
	for _, name := range utilities {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(adapters, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"awk", "wc", "cat"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(fixture.root, "observations", name)
		wrapper := "#!/bin/sh\nprintf 'used\\n' >> " + shellQuote(marker) + "\nexec " + shellQuote(path) + " \"$@\"\n"
		writeFixtureFile(t, filepath.Join(adapters, name), []byte(wrapper), 0o700)
	}
	fixture.setEnvironment("PATH", adapters)

	fixture.run(t, true)
	current := filepath.Join(fixture.binDir, "daem")
	previous := current + ".previous"
	assertFile(t, current, fixture.releases[fixtureVersion].binary)
	assertAbsent(t, previous)
	for _, name := range []string{"awk", "wc", "cat"} {
		contents, err := os.ReadFile(filepath.Join(fixture.root, "observations", name))
		if err != nil || len(contents) == 0 {
			t.Errorf("installer did not invoke %s from PATH: %v", name, err)
		}
	}

	fixture.run(t, true, "--version", fixtureVersion)
	assertFile(t, current, fixture.releases[fixtureVersion].binary)
	assertAbsent(t, previous)
	fixture.run(t, true, "--version", fixtureNext)
	assertFile(t, current, fixture.releases[fixtureNext].binary)
	assertFile(t, previous, fixture.releases[fixtureVersion].binary)

	requests := fixture.requestCount("")
	writeFixtureFile(t, filepath.Join(adapters, "curl"), []byte("#!/bin/sh\nexit 99\n"), 0o700)
	fixture.run(t, true, "--rollback")
	assertFile(t, current, fixture.releases[fixtureVersion].binary)
	assertFile(t, previous, fixture.releases[fixtureVersion].binary)
	if fixture.requestCount("") != requests {
		t.Fatal("offline rollback made a network request")
	}
}
