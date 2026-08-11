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
	for _, flags := range []int{0, 0x00080000, 0x00001000} {
		if err := validatePreparedTreeLinuxFlags("entry", flags); err != nil {
			t.Fatalf("validatePreparedTreeLinuxFlags(%#x) returned error: %v", flags, err)
		}
	}
}

func TestPreparedTreeLinuxXattrsAdmitOnlySELinuxCreationLabel(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: linuxSELinuxXattrName, want: true},
		{name: "security.ima", want: false},
		{name: "security.capability", want: false},
		{name: "system.posix_acl_access", want: false},
		{name: "user.daem", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isAllowedPreparedTreeXattr(test.name); got != test.want {
				t.Fatalf("isAllowedPreparedTreeXattr(%q) = %t, want %t", test.name, got, test.want)
			}
		})
	}
}
