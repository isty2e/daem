package archguard

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

func isTargetTypeExpression(expression ast.Expr, targetAliases map[string]bool) bool {
	switch typed := expression.(type) {
	case *ast.SelectorExpr:
		identifier, ok := typed.X.(*ast.Ident)
		return ok && targetAliases[identifier.Name] && typed.Sel.Name == "Target"
	case *ast.Ident:
		return targetAliases["."] && typed.Name == "Target"
	default:
		return false
	}
}

func isTargetLiteralConstructor(expression ast.Expr, targetAliases map[string]bool) bool {
	switch typed := expression.(type) {
	case *ast.SelectorExpr:
		identifier, ok := typed.X.(*ast.Ident)
		return ok && targetAliases[identifier.Name] && (typed.Sel.Name == "Target" || typed.Sel.Name == "ParseTarget")
	case *ast.Ident:
		return targetAliases["."] && (typed.Name == "Target" || typed.Name == "ParseTarget")
	default:
		return false
	}
}

func stringExpressionValue(expression ast.Expr) (string, bool) {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = parenthesized.X
	}
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func containsHostIdentifierVocabulary(value string) bool {
	for _, host := range []string{"claude", "codex", "opencode", "antigravity"} {
		if containsASCIIWord(value, host) {
			return true
		}
	}
	return len(value) > len("pi") && containsASCIIWord(value, "pi")
}

func containsHostVocabulary(value string) bool {
	for _, host := range []string{"claude", "codex", "opencode", "antigravity", "pi"} {
		if containsASCIIWord(value, host) {
			return true
		}
	}
	return false
}

func containsHostPathVocabulary(value string) bool {
	lower := strings.ToLower(value)
	for _, host := range []string{"claude", "codex", "opencode", "antigravity"} {
		if strings.Contains(lower, host) {
			return true
		}
	}
	return containsASCIIWord(value, "pi")
}

func containsASCIIWord(value string, word string) bool {
	for index := 0; index+len(word) <= len(value); index++ {
		if !strings.EqualFold(value[index:index+len(word)], word) {
			continue
		}
		beforeBoundary := index == 0 || !asciiLetter(value[index-1]) || (asciiLower(value[index-1]) && asciiUpper(value[index]))
		after := index + len(word)
		afterBoundary := after == len(value) || !asciiLetter(value[after]) || asciiUpper(value[after])
		if beforeBoundary && afterBoundary {
			return true
		}
	}
	return false
}

func asciiLetter(character byte) bool {
	return asciiLower(character) || asciiUpper(character)
}

func asciiLower(character byte) bool {
	return character >= 'a' && character <= 'z'
}

func asciiUpper(character byte) bool {
	return character >= 'A' && character <= 'Z'
}
