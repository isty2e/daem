package platformsupport

import (
	"errors"
	"reflect"
	"testing"
)

func TestAdmissionRowsKeepSupportAndVerificationIndependent(t *testing.T) {
	tests := []struct {
		goos         string
		goarch       string
		support      Support
		verification Verification
		admitted     bool
	}{
		{goos: "darwin", goarch: "arm64", support: SupportAdmitted, verification: VerificationNativeRequired, admitted: true},
		{goos: "linux", goarch: "amd64", support: SupportAdmitted, verification: VerificationNativeRequired, admitted: true},
		{goos: "darwin", goarch: "amd64", support: SupportNotAdmitted, verification: VerificationCompileOnly},
		{goos: "linux", goarch: "arm64", support: SupportNotAdmitted, verification: VerificationCompileOnly},
		{goos: "linux", goarch: "386", support: SupportNotAdmitted, verification: VerificationCompileOnly},
		{goos: "windows", goarch: "amd64", support: SupportNotAdmitted, verification: VerificationCompileOnly},
	}

	for _, test := range tests {
		t.Run(test.goos+"-"+test.goarch, func(t *testing.T) {
			admission, err := Lookup(test.goos, test.goarch)
			if err != nil {
				t.Fatalf("Lookup returned error: %v", err)
			}
			if admission.Target().String() != test.goos+"/"+test.goarch {
				t.Fatalf("target = %q", admission.Target())
			}
			if admission.Support() != test.support || admission.Verification() != test.verification {
				t.Fatalf("admission = %s/%s, want %s/%s", admission.Support(), admission.Verification(), test.support, test.verification)
			}
			if admission.IsAdmitted() != test.admitted {
				t.Fatalf("IsAdmitted = %t, want %t", admission.IsAdmitted(), test.admitted)
			}
		})
	}
}

func TestUnknownPlatformIsUnverifiedAndFailsClosed(t *testing.T) {
	admission, err := Lookup("freebsd", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if admission.Support() != SupportNotAdmitted || admission.Verification() != VerificationUnverified {
		t.Fatalf("unknown admission = %s/%s", admission.Support(), admission.Verification())
	}

	err = admission.RequireSupported()
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("RequireSupported error = %T %v", err, err)
	}
	if unsupported.Target().String() != "freebsd/amd64" || unsupported.Verification() != VerificationUnverified {
		t.Fatalf("unsupported facts = %s/%s", unsupported.Target(), unsupported.Verification())
	}
	want := "platform freebsd/amd64 is not an admitted daem product target (verification=unverified; admitted=darwin/arm64,linux/amd64)"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestAdmittedTargetsAreDeterministicAndDefensive(t *testing.T) {
	want := []string{"darwin/arm64", "linux/amd64"}
	first := AdmittedTargets()
	got := make([]string, 0, len(first))
	for _, target := range first {
		got = append(got, target.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AdmittedTargets = %#v, want %#v", got, want)
	}

	first[0] = Target{}
	second := AdmittedTargets()
	if second[0].String() != "darwin/arm64" {
		t.Fatalf("caller mutation changed canonical rows: %#v", second)
	}
}

func TestAdmissionCatalogHasExactRowsInCanonicalOrder(t *testing.T) {
	want := []string{
		"darwin/arm64",
		"linux/amd64",
		"darwin/amd64",
		"linux/arm64",
		"linux/386",
		"windows/amd64",
	}
	got := make([]string, 0, len(admissionRows))
	for _, admission := range admissionRows {
		got = append(got, admission.Target().String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("admission rows = %#v, want %#v", got, want)
	}
}

func TestLookupRejectsNonCanonicalTargetComponents(t *testing.T) {
	tests := [][2]string{
		{"", "amd64"},
		{"linux", ""},
		{" Linux", "amd64"},
		{"LINUX", "amd64"},
		{"linux", "amd-64"},
	}
	for _, test := range tests {
		if _, err := Lookup(test[0], test[1]); err == nil {
			t.Fatalf("Lookup(%q, %q) succeeded", test[0], test[1])
		}
	}
}

func TestParseTargetValidatesWithoutMakingAnAdmissionDecision(t *testing.T) {
	target, err := ParseTarget("freebsd", "riscv64")
	if err != nil {
		t.Fatalf("ParseTarget returned error: %v", err)
	}
	if target.String() != "freebsd/riscv64" {
		t.Fatalf("target = %s", target)
	}
	if _, err := ParseTarget("FreeBSD", "riscv64"); err == nil {
		t.Fatal("ParseTarget accepted a noncanonical target")
	}
}

func TestAdmittedRowsRequireNativeVerification(t *testing.T) {
	assertPanics(t, func() {
		mustAdmission("freebsd", "amd64", SupportAdmitted, VerificationCompileOnly)
	})
}

func TestAdmissionConstructionRejectsUnknownEnums(t *testing.T) {
	assertPanics(t, func() {
		mustAdmission("freebsd", "amd64", Support(255), VerificationUnverified)
	})
	assertPanics(t, func() {
		mustAdmission("freebsd", "amd64", SupportNotAdmitted, Verification(255))
	})
}

func TestAdmissionCatalogRejectsDuplicatesAndEmptySupport(t *testing.T) {
	notAdmitted := mustAdmission("freebsd", "amd64", SupportNotAdmitted, VerificationUnverified)
	admitted := mustAdmission("darwin", "arm64", SupportAdmitted, VerificationNativeRequired)
	assertPanics(t, func() {
		mustAdmissionCatalog(notAdmitted, notAdmitted)
	})
	assertPanics(t, func() {
		mustAdmissionCatalog(notAdmitted)
	})
	assertPanics(t, func() {
		mustAdmissionCatalog(admitted, Admission{})
	})
}

func TestZeroAdmissionFailsClosed(t *testing.T) {
	var admission Admission
	if admission.IsAdmitted() {
		t.Fatal("zero admission was admitted")
	}
	if admission.Target().String() != "unknown/unknown" || admission.Verification() != VerificationUnverified {
		t.Fatalf("zero admission = %s/%s", admission.Target(), admission.Verification())
	}
	if admission.RequireSupported() == nil {
		t.Fatal("zero admission passed support gate")
	}
}

func assertPanics(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	operation()
}
