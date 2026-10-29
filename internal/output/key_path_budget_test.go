package output_test

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestBoundedKeyPathUsesEncodedBytes(t *testing.T) {
	for _, fixture := range []struct {
		keys []string
		want string
	}{
		{nil, ""},
		{[]string{""}, "/"},
		{[]string{"a/~", "", "한글"}, "/a~1~0//한글"},
	} {
		maximum := int64(len(fixture.want))
		key, err := output.NewKeyPathWithinBytes(maximum, fixture.keys...)
		if err != nil || key.String() != fixture.want {
			t.Fatalf("bounded pointer differs: %q, %v", key, err)
		}
		if maximum != 0 {
			key, err := output.NewKeyPathWithinBytes(maximum-1, fixture.keys...)
			if !errors.Is(err, output.ErrKeyPathLimitExceeded) || !key.IsRoot() {
				t.Fatal("over-budget pointer returned partial identity")
			}
		}
	}
	for _, fixture := range []struct {
		maximum int64
		key     string
	}{{-1, "valid"}, {64, "\xff"}} {
		key, err := output.NewKeyPathWithinBytes(fixture.maximum, fixture.key)
		if err == nil || !key.IsRoot() {
			t.Fatal("invalid bound or UTF-8 acquired a pointer")
		}
	}
}
