package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/platformsupport"
)

func TestAssessObservesOnlyAdmittedTargetsWithRuntimeRequirements(t *testing.T) {
	darwin := testAdmission(t, "darwin", "arm64")
	minimum, required := darwin.RuntimeRequirement()
	if !required {
		t.Fatal("Darwin runtime requirement is missing")
	}
	observation, err := platformsupport.NewRuntimeObservation(minimum)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	observer := func(context.Context) (platformsupport.RuntimeObservation, error) {
		calls++
		return observation, nil
	}
	assessment, err := Assess(context.Background(), darwin, observer)
	if err != nil || !assessment.IsAdmitted() || calls != 1 {
		t.Fatalf("Darwin assessment error=%v admitted=%t calls=%d", err, assessment.IsAdmitted(), calls)
	}

	for _, test := range []struct {
		goos         string
		goarch       string
		wantAdmitted bool
	}{
		{goos: "linux", goarch: "amd64", wantAdmitted: true},
		{goos: "windows", goarch: "amd64"},
	} {
		assessment, err = Assess(context.Background(), testAdmission(t, test.goos, test.goarch), observer)
		if err != nil {
			t.Fatalf("Assess(%s/%s): %v", test.goos, test.goarch, err)
		}
		if assessment.IsAdmitted() != test.wantAdmitted {
			t.Fatalf("Assess(%s/%s).IsAdmitted() = %t, want %t", test.goos, test.goarch, assessment.IsAdmitted(), test.wantAdmitted)
		}
	}
	if calls != 1 {
		t.Fatalf("observer calls = %d, want 1", calls)
	}
}

func TestAssessPropagatesCancellationWithoutObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := Assess(ctx, testAdmission(t, "darwin", "arm64"), func(context.Context) (platformsupport.RuntimeObservation, error) {
		called = true
		return platformsupport.RuntimeObservation{}, nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("error=%v called=%t, want canceled without observer", err, called)
	}
}

func TestAssessRequiresContext(t *testing.T) {
	if _, err := Assess(nil, testAdmission(t, "darwin", "arm64"), nil); err == nil {
		t.Fatal("Assess accepted nil context")
	}
}

func testAdmission(t *testing.T, goos string, goarch string) platformsupport.Admission {
	t.Helper()
	admission, err := platformsupport.Lookup(goos, goarch)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}
