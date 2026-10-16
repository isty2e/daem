package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"slices"
	"testing"
)

func argumentPlanFlags() *flag.FlagSet {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	flags.Bool("json", false, "format")
	flags.Bool("yes", false, "authorization")
	flags.Bool("dry-run", false, "mode")
	flags.String("manifest", "", "path")
	flags.String("arg", "", "command data")
	return flags
}

func TestGoArgumentPlanSelectsFormatFromRoles(t *testing.T) {
	for _, test := range []struct {
		args []string
		want requestedOutput
	}{
		{nil, requestedHuman},
		{[]string{"--json"}, requestedJSON},
		{[]string{"-json"}, requestedJSON},
		{[]string{"--json=true"}, requestedJSON},
		{[]string{"--json", "--json=false"}, requestedHuman},
		{[]string{"--json=maybe"}, requestedHuman},
		{[]string{"--json", "--json=maybe"}, requestedJSON},
		{[]string{"--json", "--json=maybe", "--json=false"}, requestedHuman},
		{[]string{"--targte", "bad", "--json"}, requestedJSON},
		{[]string{"--json", "--targte", "bad"}, requestedJSON},
		{[]string{"unbound", "--json"}, requestedJSON},
		{[]string{"--manifest", "--json"}, requestedHuman},
		{[]string{"--arg=--json"}, requestedHuman},
		{[]string{"--", "--json"}, requestedHuman},
		{[]string{"--unknown", "--", "--json"}, requestedHuman},
		{[]string{"--manifest", "--", "--json"}, requestedJSON},
		{[]string{"--manifest", "--", "--help", "--json"}, requestedJSON},
		{[]string{"---json"}, requestedHuman},
		{[]string{"---json", "--json"}, requestedJSON},
		{[]string{"--=true", "--json"}, requestedJSON},
		{[]string{"-=true", "--json"}, requestedJSON},
	} {
		t.Run(flagCaseName(test.args), func(t *testing.T) {
			plan := prepareGoArguments(argumentPlanFlags(), test.args)
			if plan.output != test.want {
				t.Fatalf("args=%q output=%v, want %v", test.args, plan.output, test.want)
			}
		})
	}
}

func flagCaseName(args []string) string {
	if len(args) == 0 {
		return "no arguments"
	}
	var output bytes.Buffer
	for _, argument := range args {
		output.WriteString(argument)
		output.WriteByte(' ')
	}
	return output.String()
}

type argumentPlanValue struct {
	calls int
	err   error
}

func (value *argumentPlanValue) Set(string) error {
	value.calls++
	return value.err
}

func (*argumentPlanValue) String() string { return "" }

func TestArgumentPlanningDoesNotBindValuesOrPublish(t *testing.T) {
	var stderr bytes.Buffer
	flags := newCommandFlagSet([]string{"status"}, &stderr)
	scratchJSON := flags.Bool("json", false, "format")
	cause := errors.New("target rejected")
	target := &argumentPlanValue{err: cause}
	flags.Var(target, "target", "selector")
	args := []string{"--target", "bad", "--unknown", "--json"}
	plan := prepareGoArguments(flags, args)
	args[0] = "changed"
	if target.calls != 0 || stderr.Len() != 0 || *scratchJSON || plan.output != requestedJSON {
		t.Fatal("preparation performed binding/publication or lost format")
	}
	if err := plan.bind(flags); err == nil || target.calls != 1 || stderr.Len() != 0 {
		t.Fatalf("binding lost the earlier value failure or leaked output: %v, %d, %q", err, target.calls, stderr.String())
	}
}

func TestArgumentPlanModeSurvivesFailedBoolBindingAndParserHelp(t *testing.T) {
	flags := newCommandFlagSet([]string{"status"}, io.Discard)
	mutable := flags.Bool("json", false, "format")
	plan := prepareGoArguments(flags, []string{"--json", "--json=maybe"})
	if err := plan.bind(flags); err == nil || *mutable || plan.output != requestedJSON {
		t.Fatalf("partial bool storage replaced requested format: %v, %t, %v", err, *mutable, plan.output)
	}

	flags = argumentPlanFlags()
	plan = prepareGoArguments(flags, []string{"--manifest", "--", "--help", "--json"})
	if err := plan.bind(flags); !errors.Is(err, flag.ErrHelp) || plan.output != requestedJSON {
		t.Fatalf("implicit parser help or protected value changed: %v, %v", err, plan.output)
	}
}

func TestAuthoringArgumentPlanPreservesShapePrecedence(t *testing.T) {
	flags := argumentPlanFlags()
	target := &argumentPlanValue{err: errors.New("target rejected")}
	flags.Var(target, "target", "selector")
	plan := prepareAuthoringArguments(flags, []string{"--target", "bad", "--manfiest", "daem.toml", "source", "--json"}, 1)
	var optionErr *optionArgumentError
	if err := plan.bind(flags); !errors.As(err, &optionErr) || optionErr.kind != optionUnknown || target.calls != 0 || plan.output != requestedJSON {
		t.Fatalf("binding or overflow replaced the split failure: %v, %d, %v", err, target.calls, plan.output)
	}
	if operands, err := plan.operands(); err == nil || operands != nil {
		t.Fatal("failed shape exposed executable operands")
	}
}

func TestAuthoringArgumentPlanPreservesInterspersedValuesAndTerminator(t *testing.T) {
	flags := argumentPlanFlags()
	plan := prepareAuthoringArguments(flags, []string{"source", "--arg", "--json", "--", "--json"}, 2)
	operands, err := plan.operands()
	if err != nil || !slices.Equal(operands, []string{"source", "--json"}) || plan.output != requestedHuman {
		t.Fatalf("declared data acquired format authority: %q, %v, %v", operands, err, plan.output)
	}
	operands[0] = "changed"
	if again, _ := plan.operands(); again[0] != "source" {
		t.Fatal("caller changed retained operands")
	}
	if err := plan.bind(flags); err != nil || flags.Lookup("arg").Value.String() != "--json" {
		t.Fatalf("interspersed value binding changed: %v", err)
	}
}

func TestAuthoringArgumentPlanDoesNotAdmitGoOnlySpellings(t *testing.T) {
	flags := argumentPlanFlags()
	plan := prepareAuthoringArguments(flags, []string{"source", "-json", "--json"}, 1)
	var optionErr *optionArgumentError
	if err := plan.bind(flags); !errors.As(err, &optionErr) || optionErr.kind != optionUnknown || plan.output != requestedJSON {
		t.Fatalf("unsupported spelling was admitted or hid later format: %v, %v", err, plan.output)
	}
}

func TestNamedArgumentPlanDoesNotFillAnAbsentNameFromLaterData(t *testing.T) {
	flags := argumentPlanFlags()
	plan := prepareNamedArguments(flags, []string{"--yes", "later-name", "--dry-run", "--json"}, "mcp-server name")
	var missing *missingNamedArgumentError
	if err := plan.bind(flags); !errors.As(err, &missing) || plan.name != "" || plan.options.output != requestedJSON {
		t.Fatalf("option or later data filled the required slot: %v, %q, %v", err, plan.name, plan.options.output)
	}
	if flags.Lookup("yes").Value.String() != "false" {
		t.Fatal("missing-name failure performed semantic binding")
	}

	plan = prepareNamedArguments(flags, []string{"server", "--manifest", "--json"}, "mcp-server name")
	if err := plan.bind(flags); err != nil || plan.name != "server" || plan.options.output != requestedHuman {
		t.Fatalf("valid fixed name/value interpretation changed: %v", err)
	}
}

func TestVersionArgumentPlanKeepsExactValidGrammar(t *testing.T) {
	for _, test := range []struct {
		args []string
		mode requestedOutput
		bad  bool
	}{
		{nil, requestedHuman, false},
		{[]string{"--json"}, requestedJSON, false},
		{[]string{"extra", "--json"}, requestedJSON, true},
		{[]string{"--json", "--json"}, requestedJSON, true},
		{[]string{"--json=false"}, requestedHuman, true},
		{[]string{"--", "--json"}, requestedHuman, true},
	} {
		plan := prepareVersionArguments(test.args)
		if plan.output != test.mode || (plan.err != nil) != test.bad {
			t.Errorf("args=%q mode=%v, err=%v", test.args, plan.output, plan.err)
		}
	}
}
