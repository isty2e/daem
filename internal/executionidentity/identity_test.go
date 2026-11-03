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
	context, err := NewContextWithVisibility(Darwin, strings.Repeat("b", 64), "12345678-1234-1234-1234-123456789abc", strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewBinding(invocation, context, 123)
	if err != nil || binding.Invocation() != invocation || binding.Context() != context || binding.Group() != 123 {
		t.Fatalf("binding did not preserve its observed identity: %v", err)
	}
	otherEpoch, err := NewContextWithVisibility(Darwin, context.Realm(), "22345678-1234-1234-1234-123456789abc", strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	otherRealm, err := NewContextWithVisibility(Darwin, strings.Repeat("c", 64), context.Epoch(), context.Visibility())
	if err != nil {
		t.Fatal(err)
	}
	otherPlatform, err := NewContextWithVisibility(Linux, context.Realm(), context.Epoch(), context.Visibility())
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

func TestContextVisibilityIsIndependentFromStableRealmAndBoot(t *testing.T) {
	realm := strings.Repeat("a", 64)
	epoch := "12345678-1234-1234-1234-123456789abc"
	visibility := strings.Repeat("b", 64)
	known, err := NewContextWithVisibility(Linux, realm, epoch, visibility)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := NewContext(Linux, realm, epoch)
	if err != nil {
		t.Fatal(err)
	}
	otherNamespace, err := NewContextWithVisibility(Linux, realm, epoch, strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	afterRestart, err := NewContextWithVisibility(Linux, realm, "22345678-1234-1234-1234-123456789abc", otherNamespace.Visibility())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		other Context
		want  ContextRelation
	}{{known, SameEpoch}, {legacy, Incomparable}, {otherNamespace, Incomparable}, {afterRestart, DifferentEpoch}} {
		if known.Relation(test.other) != test.want || test.other.Relation(known) != test.want {
			t.Fatal("context comparison collapsed host, boot or namespace facts")
		}
	}
	if !known.HasVisibility() || known.Visibility() != visibility || legacy.HasVisibility() || legacy.Relation(legacy) != Incomparable {
		t.Fatal("legacy context acquired process-visibility evidence")
	}
	for _, invalid := range []string{"", strings.Repeat("0", 64), strings.Repeat("B", 64), strings.Repeat("b", 63)} {
		if _, err := NewContextWithVisibility(Linux, realm, epoch, invalid); err == nil {
			t.Fatal("invalid visibility was admitted as a qualified fact")
		}
	}
}
