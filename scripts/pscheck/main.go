package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pscheck <scripts/*.ps1>")
		os.Exit(2)
	}
	var failed bool
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		if err := checkPowerShellStructure(string(data)); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("pwsh not found; PowerShell scripts passed structural fallback check.")
}

func checkPowerShellStructure(input string) error {
	stack := make([]rune, 0)
	inSingle, inDouble, inComment := false, false, false
	var previous rune
	for _, r := range input {
		if inComment {
			if r == '\n' || r == '\r' {
				inComment = false
			}
			previous = r
			continue
		}
		if inSingle {
			if r == '\'' && previous != '\'' {
				inSingle = false
			}
			previous = r
			continue
		}
		if inDouble {
			if r == '"' && previous != '`' {
				inDouble = false
			}
			previous = r
			continue
		}
		switch r {
		case '#':
			inComment = true
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '{', '(', '[':
			stack = append(stack, r)
		case '}':
			if !popMatches(&stack, '{') {
				return fmt.Errorf("unmatched }")
			}
		case ')':
			if !popMatches(&stack, '(') {
				return fmt.Errorf("unmatched )")
			}
		case ']':
			if !popMatches(&stack, '[') {
				return fmt.Errorf("unmatched ]")
			}
		}
		previous = r
	}
	if inSingle {
		return fmt.Errorf("unterminated single-quoted string")
	}
	if inDouble {
		return fmt.Errorf("unterminated double-quoted string")
	}
	if len(stack) > 0 {
		return fmt.Errorf("unclosed %c", stack[len(stack)-1])
	}
	return nil
}

func popMatches(stack *[]rune, expected rune) bool {
	if len(*stack) == 0 {
		return false
	}
	last := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	return last == expected
}
