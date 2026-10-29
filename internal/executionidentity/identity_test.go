package executionidentity

import (
	"strings"
	"testing"
)

func TestIdentityAdmissionAndContextComparison(t *testing.T) {
	invocation, err := NewInvocation(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewContext(Darwin, strings.Repeat("b", 64), "12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewBinding(invocation, context, 123)
	if err != nil || binding.Invocation() != invocation || binding.Context() != context || binding.Group() != 123 {
		t.Fatalf("binding did not preserve its observed identity: %v", err)
	}
	otherEpoch, err := NewContext(Darwin, context.Realm(), "22345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	otherRealm, err := NewContext(Darwin, strings.Repeat("c", 64), context.Epoch())
	if err != nil {
		t.Fatal(err)
	}
	otherPlatform, err := NewContext(Linux, context.Realm(), context.Epoch())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		other Context
		want  ContextRelation
	}{{context, SameEpoch}, {otherEpoch, DifferentEpoch}, {otherRealm, Incomparable}, {otherPlatform, Incomparable}, {Context{}, Incomparable}} {
		if got := context.Relation(test.other); got != test.want || test.other.Relation(context) != test.want {
			t.Fatalf("context relation = %v, want %v", got, test.want)
		}
	}
	if (Context{}).Relation(Context{}) != Incomparable {
		t.Fatal("zero contexts provided epoch evidence")
	}
}

func TestIdentityRefusesMalformedOrIncompleteObservations(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("0", 32), strings.Repeat("A", 32), strings.Repeat("a", 31), "../" + strings.Repeat("a", 29)} {
		if _, err := NewInvocation(value); err == nil {
			t.Fatal("malformed invocation identity was admitted")
		}
	}
	validRealm := strings.Repeat("a", 64)
	validEpoch := "12345678-1234-1234-1234-123456789abc"
	for _, test := range []struct {
		platform Platform
		realm    string
		epoch    string
	}{
		{"", validRealm, validEpoch},
		{"windows", validRealm, validEpoch},
		{Linux, strings.Repeat("0", 64), validEpoch},
		{Linux, strings.ToUpper(validRealm), validEpoch},
		{Linux, validRealm, strings.ToUpper(validEpoch)},
		{Linux, validRealm, strings.ReplaceAll(validEpoch, "-", "")},
		{Linux, validRealm, "00000000-0000-0000-0000-000000000000"},
		{Linux, validRealm, "12345678-1234-1234-1234-123456789ab-"},
	} {
		if _, err := NewContext(test.platform, test.realm, test.epoch); err == nil {
			t.Fatal("malformed execution context was admitted")
		}
	}
	invocation, _ := NewInvocation(strings.Repeat("a", 32))
	context, _ := NewContext(Linux, validRealm, validEpoch)
	for _, group := range []int64{-1, 0, 1 << 31} {
		if _, err := NewBinding(invocation, context, group); err == nil {
			t.Fatal("invalid process-group observation was admitted")
		}
	}
	if _, err := NewBinding(Invocation{}, context, 123); err == nil {
		t.Fatal("zero invocation was admitted")
	}
	if _, err := NewBinding(invocation, Context{}, 123); err == nil {
		t.Fatal("zero execution context was admitted")
	}
}
