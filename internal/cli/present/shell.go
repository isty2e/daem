package clipresent

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const shellSafeTokenCharacters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+,:./-"

var errShellArgumentContainsNUL = errors.New("shell command argument contains NUL")

// ShellCommand renders one argv vector for copy-paste execution by a POSIX shell.
// Control-bearing arguments are reconstructed inside a subshell so the rendered
// text stays terminal-safe without changing the resulting argv.
func ShellCommand(argv ...string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("shell command requires at least one argument")
	}

	arguments := make([]string, 0, len(argv))
	initializers := make([]string, 0)
	for index, argument := range argv {
		if strings.ContainsRune(argument, '\x00') {
			return "", fmt.Errorf("argument %d: %w", index, errShellArgumentContainsNUL)
		}
		if shellArgumentRequiresReconstruction(argument) {
			variable := fmt.Sprintf("_daem_arg_%d", index)
			initializers = append(
				initializers,
				variable+"=$(printf '%b_' "+shellArgument(shellByteEscapes(argument))+")",
				variable+"=${"+variable+"%_}",
			)
			arguments = append(arguments, `"$`+variable+`"`)
			continue
		}
		arguments = append(arguments, shellArgument(argument))
	}

	command := strings.Join(arguments, " ")
	if len(initializers) == 0 {
		return command, nil
	}
	return "(" + strings.Join(append(initializers, command), "; ") + ")", nil
}

// PrintShellCommand writes a concrete command only when every argv entry can be
// represented exactly. Callers retain ownership of any non-command fallback.
func PrintShellCommand(output io.Writer, prefix string, argv ...string) {
	command, err := ShellCommand(argv...)
	if err != nil {
		return
	}
	fmt.Fprintf(output, "%s%s\n", prefix, command)
}

func shellArgument(argument string) string {
	if argument != "" && strings.IndexFunc(argument, func(char rune) bool {
		return char > 127 || !strings.ContainsRune(shellSafeTokenCharacters, char)
	}) == -1 {
		return argument
	}
	return "'" + strings.ReplaceAll(argument, "'", `'"'"'`) + "'"
}

func shellArgumentRequiresReconstruction(argument string) bool {
	for len(argument) > 0 {
		character, size := utf8.DecodeRuneInString(argument)
		if character == utf8.RuneError && size == 1 {
			return true
		}
		if unicode.IsControl(character) || !unicode.IsGraphic(character) {
			return true
		}
		argument = argument[size:]
	}
	return false
}

func shellByteEscapes(argument string) string {
	var encoded strings.Builder
	encoded.Grow(len(argument) * 5)
	for _, value := range []byte(argument) {
		encoded.WriteString(`\0`)
		encoded.WriteByte('0' + (value>>6)&7)
		encoded.WriteByte('0' + (value>>3)&7)
		encoded.WriteByte('0' + value&7)
	}
	return encoded.String()
}
