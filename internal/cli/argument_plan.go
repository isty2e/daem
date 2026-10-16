package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

type requestedOutput uint8

const (
	requestedHuman requestedOutput = iota
	requestedJSON
)

type optionSpelling uint8

const (
	goOptionSpelling optionSpelling = iota
	authoringOptionSpelling
)

type argumentTokenKind uint8

const (
	argumentOperand argumentTokenKind = iota
	argumentOption
	argumentTerminator
	argumentInvalid
)

type argumentToken struct {
	kind  argumentTokenKind
	words []string
	err   error
}

type optionArgumentFailure uint8

const (
	optionUnknown optionArgumentFailure = iota
	optionMalformed
	optionValueMissing
)

type optionArgumentError struct {
	kind     optionArgumentFailure
	argument string
}

func (err *optionArgumentError) Error() string {
	switch err.kind {
	case optionMalformed:
		return fmt.Sprintf("bad flag syntax: %s", err.argument)
	case optionValueMissing:
		return fmt.Sprintf("flag needs an argument: %s", err.argument)
	default:
		return fmt.Sprintf("flag provided but not defined: %s", err.argument)
	}
}

// optionCursor reads declaration metadata only. Invalid unbound data does not
// terminate format recovery; declared values and an unconsumed -- remain opaque.
type optionCursor struct {
	flags          *flag.FlagSet
	arguments      []string
	spelling       optionSpelling
	index          int
	positionalOnly bool
	output         requestedOutput
}

func (cursor *optionCursor) next() (argumentToken, bool) {
	if cursor.index == len(cursor.arguments) {
		return argumentToken{}, false
	}
	start := cursor.index
	argument := cursor.arguments[start]
	cursor.index++
	word := cursor.arguments[start:cursor.index]
	if cursor.positionalOnly || !strings.HasPrefix(argument, "-") || argument == "-" {
		return argumentToken{kind: argumentOperand, words: word}, true
	}
	if argument == "--" {
		cursor.positionalOnly = true
		return argumentToken{kind: argumentTerminator, words: word}, true
	}
	if cursor.spelling == authoringOptionSpelling {
		if argument == "-h" || argument == "--help" {
			return argumentToken{kind: argumentOption, words: word}, true
		}
		if !strings.HasPrefix(argument, "--") {
			return argumentToken{kind: argumentInvalid, words: word, err: &optionArgumentError{kind: optionUnknown, argument: argument}}, true
		}
	}

	name := strings.TrimPrefix(argument, "-")
	name = strings.TrimPrefix(name, "-")
	if name == "" || name[0] == '-' || name[0] == '=' {
		return argumentToken{kind: argumentInvalid, words: word, err: &optionArgumentError{kind: optionMalformed, argument: argument}}, true
	}
	name, value, inline := strings.Cut(name, "=")
	declared := cursor.flags.Lookup(name)
	if declared == nil {
		if cursor.spelling == goOptionSpelling && (name == "h" || name == "help") {
			return argumentToken{kind: argumentOption, words: word}, true
		}
		return argumentToken{kind: argumentInvalid, words: word, err: &optionArgumentError{kind: optionUnknown, argument: "-" + name}}, true
	}

	boolean, isBoolean := declared.Value.(interface{ IsBoolFlag() bool })
	if isBoolean && boolean.IsBoolFlag() {
		if name == "json" {
			if !inline {
				value = "true"
			}
			if enabled, err := strconv.ParseBool(value); err == nil {
				cursor.output = requestedHuman
				if enabled {
					cursor.output = requestedJSON
				}
			}
		}
		return argumentToken{kind: argumentOption, words: word}, true
	}
	if !inline {
		if cursor.index == len(cursor.arguments) {
			return argumentToken{kind: argumentInvalid, words: word, err: &optionArgumentError{kind: optionValueMissing, argument: "-" + name}}, true
		}
		cursor.index++
	}
	return argumentToken{kind: argumentOption, words: cursor.arguments[start:cursor.index]}, true
}

type goArgumentPlan struct {
	output    requestedOutput
	arguments []string
}

func prepareGoArguments(flags *flag.FlagSet, arguments []string) goArgumentPlan {
	words := slices.Clone(arguments)
	cursor := optionCursor{flags: flags, arguments: words}
	for {
		if _, present := cursor.next(); !present {
			break
		}
	}
	return goArgumentPlan{output: cursor.output, arguments: words}
}

func (plan goArgumentPlan) bind(flags *flag.FlagSet) error {
	return bindCommandFlags(flags, plan.arguments)
}

type authoringArgumentPlan struct {
	output      requestedOutput
	options     []string
	positionals []string
	shapeError  error
}

func prepareAuthoringArguments(flags *flag.FlagSet, arguments []string, positionalCount int) authoringArgumentPlan {
	plan := authoringArgumentPlan{}
	cursor := optionCursor{flags: flags, arguments: slices.Clone(arguments), spelling: authoringOptionSpelling}
	for {
		token, present := cursor.next()
		if !present {
			break
		}
		switch token.kind {
		case argumentOperand:
			if len(plan.positionals) == positionalCount {
				if plan.shapeError == nil {
					plan.shapeError = &unexpectedArgumentError{argument: token.words[0]}
				}
			} else {
				plan.positionals = append(plan.positionals, token.words[0])
			}
		case argumentOption:
			plan.options = append(plan.options, token.words...)
		case argumentInvalid:
			if plan.shapeError == nil {
				plan.shapeError = token.err
			}
		case argumentTerminator:
		}
	}
	plan.output = cursor.output
	return plan
}

func (plan authoringArgumentPlan) operands() ([]string, error) {
	if plan.shapeError != nil {
		return nil, plan.shapeError
	}
	return slices.Clone(plan.positionals), nil
}

func (plan authoringArgumentPlan) bind(flags *flag.FlagSet) error {
	if plan.shapeError != nil {
		return plan.shapeError
	}
	return bindCommandFlags(flags, plan.options)
}

type missingNamedArgumentError struct {
	label string
}

func (err *missingNamedArgumentError) Error() string {
	return err.label + " is required"
}

type namedArgumentPlan struct {
	options goArgumentPlan
	name    string
	missing *missingNamedArgumentError
}

func prepareNamedArguments(flags *flag.FlagSet, arguments []string, label string) namedArgumentPlan {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "-") {
		return namedArgumentPlan{
			options: prepareGoArguments(flags, arguments),
			missing: &missingNamedArgumentError{label: label},
		}
	}
	return namedArgumentPlan{options: prepareGoArguments(flags, arguments[1:]), name: arguments[0]}
}

func (plan namedArgumentPlan) bind(flags *flag.FlagSet) error {
	if plan.missing != nil {
		return plan.missing
	}
	return plan.options.bind(flags)
}

type versionArgumentPlan struct {
	output requestedOutput
	err    error
}

func prepareVersionArguments(arguments []string) versionArgumentPlan {
	plan := versionArgumentPlan{}
	for _, argument := range arguments {
		if argument == "--" {
			break
		}
		if argument == "--json" {
			plan.output = requestedJSON
		}
	}
	if len(arguments) != 0 && arguments[0] != "--json" {
		plan.err = &unexpectedArgumentError{argument: arguments[0]}
	} else if len(arguments) > 1 {
		plan.err = &unexpectedArgumentError{argument: arguments[1]}
	}
	return plan
}

// Parsing owns no process output. In particular, replacing Output alone cannot
// isolate a Usage closure that captured the original stderr.
func bindCommandFlags(flags *flag.FlagSet, arguments []string) error {
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	return flags.Parse(arguments)
}
