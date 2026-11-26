//go:build linux

package commit

import "testing"

func TestPreparedTreeLinuxFlagsRejectUnrepresentedSemantics(t *testing.T) {
	tests := []struct {
		name  string
		flags int
	}{
		{name: "no copy-on-write with compression disabled", flags: 0x00800000 | linuxNoCompress},
		{name: "case-folded directory", flags: 0x40000000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := preparedTreeLinuxFlagFacts("entry", test.flags); !isUnsupported(err) {
				t.Fatalf("flags %#x: error = %v, want unsupported", test.flags, err)
			}
		})
	}
	for _, flags := range []int{0, 0x00080000, 0x00001000} {
		for _, ignored := range []int{0, linuxNoDump, linuxNoCompress, linuxNoDump | linuxNoCompress} {
			if facts, err := preparedTreeLinuxFlagFacts("entry", flags|ignored); err != nil || facts != uint64(flags) {
				t.Fatalf("flags %#x: facts = %#x, error = %v; want %#x", flags|ignored, facts, err, flags)
			}
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
