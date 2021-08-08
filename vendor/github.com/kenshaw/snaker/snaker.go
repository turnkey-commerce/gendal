// Package snaker provides methods to convert CamelCase to and from snake_case.
//
// Correctly recognizes common (Go idiomatic) initialisms (HTTP, XML, etc) and
// provides a mechanism to override/set recognized initialisms.
package snaker

import (
	"regexp"
	"strings"
	"unicode"
)

func init() {
	// initialize common default initialisms.
	DefaultInitialisms, _ = New(CommonInitialisms()...)
}

// CommonInitialisms returns the set of common initialisms.
//
// Originally built from the list in golang.org/x/lint @ 738671d.
//
// Note: golang.org/x/lint has since been deprecated, and some additional
// initialisms have since been added.
func CommonInitialisms() []string {
	return []string{
		"ACL",
		"API",
		"ASCII",
		"CPU",
		"CSS",
		"DNS",
		"EOF",
		"GUID",
		"HTML",
		"HTTPS",
		"HTTP",
		"ID",
		"IP",
		"JSON",
		"LHS",
		"QPS",
		"RAM",
		"RHS",
		"RPC",
		"SLA",
		"SMTP",
		"SQL",
		"SSH",
		"TCP",
		"TLS",
		"TTL",
		"UDP",
		"UID",
		"UI",
		"URI",
		"URL",
		"UTC",
		"UTF8",
		"UUID",
		"VM",
		"XML",
		"XMPP",
		"XSRF",
		"XSS",
		"YAML",
	}
}

// DefaultInitialisms is a set of default (common) initialisms.
var DefaultInitialisms *Initialisms

// CamelToSnake converts name from camel case ("AnIdentifier") to snake case
// ("an_identifier").
func CamelToSnake(name string) string {
	return DefaultInitialisms.CamelToSnake(name)
}

// CamelToSnakeIdentifier converts name from camel case to a snake case
// identifier.
func CamelToSnakeIdentifier(name string) string {
	return DefaultInitialisms.CamelToSnakeIdentifier(name)
}

// SnakeToCamel converts name to CamelCase.
func SnakeToCamel(name string) string {
	return DefaultInitialisms.SnakeToCamel(name)
}

// SnakeToCamelIdentifier converts name to its CamelCase identifier (first
// letter is capitalized).
func SnakeToCamelIdentifier(name string) string {
	return DefaultInitialisms.SnakeToCamelIdentifier(name)
}

// ForceCamelIdentifier forces name to its CamelCase specific to Go
// ("AnIdentifier").
func ForceCamelIdentifier(name string) string {
	return DefaultInitialisms.ForceCamelIdentifier(name)
}

// ForceLowerCamelIdentifier forces the first portion of an identifier to be
// lower case ("anIdentifier").
func ForceLowerCamelIdentifier(name string) string {
	return DefaultInitialisms.ForceLowerCamelIdentifier(name)
}

// Peek returns the next longest possible initialism in r.
func Peek(r []rune) string {
	return DefaultInitialisms.Peek(r)
}

// IsInitialism indicates whether or not s is a registered initialism.
func IsInitialism(s string) bool {
	return DefaultInitialisms.IsInitialism(s)
}

// IsIdentifierChar determines if ch is a valid character for a Go identifier.
//
// See: go/src/go/scanner/scanner.go
func IsIdentifierChar(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_' || ch >= 0x80 && unicode.IsLetter(ch) ||
		'0' <= ch && ch <= '9' || ch >= 0x80 && unicode.IsDigit(ch)
}

// ToIdentifier cleans s so that it is usable as an identifier.
//
// Substitutes invalid characters with an underscore, removes any leading
// numbers/underscores, and removes trailing underscores.
//
// Additionally collapses multiple underscores to a single underscore.
//
// Makes no changes to case.
func ToIdentifier(s string) string {
	// replace bad chars with _
	s = subUnderscores(strings.TrimSpace(s))
	// fix 2 or more __ and remove leading numbers/underscores
	s = underscoreRE.ReplaceAllString(s, "_")
	s = leadingRE.ReplaceAllString(s, "_")
	// remove leading/trailing underscores
	s = strings.TrimLeft(s, "_")
	s = strings.TrimRight(s, "_")
	return s
}

// underscoreRE matches underscores.
var underscoreRE = regexp.MustCompile(`_+`)

// leadingRE matches leading numbers.
var leadingRE = regexp.MustCompile(`^[0-9_]+`)

// subUnderscores substitues underscrose in place of runes that are invalid for
// Go identifiers.
func subUnderscores(s string) string {
	var r []rune
	for _, c := range s {
		if IsIdentifierChar(c) {
			r = append(r, c)
		} else {
			r = append(r, '_')
		}
	}
	return string(r)
}
