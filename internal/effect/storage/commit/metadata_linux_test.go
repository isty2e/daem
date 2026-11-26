//go:build linux

package commit

import "testing"

func TestPreparedTreeLinuxFlagsRejectUnrepresentedSemantics(t *testing.T) {
	tests := []struct {
		name  string
		flags int
	}{
		{name: "no copy-on-write", flags: 0x00800000},
		{name: "case-folded directory", flags: 0x40000000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePreparedTreeLinuxFlags("entry", test.flags); !isUnsupported(err) {
				t.Fatalf("validatePreparedTreeLinuxFlags(%#x) error = %v, want unsupported", test.flags, err)
			}
		})
	}
	for _, flags := range []int{0, 0x00080000, 0x00001000, linuxNoDump} {
		if err := validatePreparedTreeLinuxFlags("entry", flags); err != nil {
			t.Fatalf("validatePreparedTreeLinuxFlags(%#x) returned error: %v", flags, err)
		}
	}
}

func TestPreparedTreeInspectsLinuxAccessMetadata(t *testing.T) {
	tests := []struct {
		name   string
		read   bool
		refuse bool
	}{
		{name: "security.selinux", read: true},
		{name: "security.ima"},
		{name: "security.capability", refuse: true},
		{name: "system.posix_acl_access", refuse: true},
		{name: "system.posix_acl_default", refuse: true},
		{name: "user.daem"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			read := false
			facts, err := capturePreparedTreeXattrFacts("entry", []string{test.name}, func(string) ([]byte, error) { read = true; return []byte("value"), nil })
			if (err != nil) != test.refuse || (test.refuse && !isUnsupported(err)) || read != test.read || (len(facts) == 1) != test.read {
				t.Fatalf("read=%t facts=%v error=%v", read, facts, err)
			}
		})
	}
}
