package snaker

import (
	"fmt"
	"strings"
	"unicode"
)

// Initialisms is a set of initialisms.
type Initialisms struct {
	m   map[string]bool
	max int
}

// New creates a new set of initialisms.
func New(initialisms ...string) (*Initialisms, error) {
	ini := &Initialisms{
		m: make(map[string]bool),
	}
	if err := ini.Add(initialisms...); err != nil {
		return nil, err
	}
	return ini, nil
}

// Add adds initialisms.
func (ini *Initialisms) Add(initialisms ...string) error {
	for _, s := range initialisms {
		if len(s) < 2 {
			return fmt.Errorf("invalid initialism %q", s)
		}
		ini.m[s], ini.max = true, max(ini.max, len(s))
	}
	return nil
}

// CamelToSnake converts name from camel case ("AnIdentifier") to snake case
// ("an_identifier").
func (ini *Initialisms) CamelToSnake(name string) string {
	if name == "" {
		return ""
	}
	s, r := "", []rune(name)
	var lastWasUpper, lastWasLetter, lastWasIsm, isUpper, isLetter bool
	for i := 0; i < len(r); {
		isUpper, isLetter = unicode.IsUpper(r[i]), unicode.IsLetter(r[i])
		// append _ when last was not upper and not letter
		if (lastWasLetter && isUpper) || (lastWasIsm && isLetter) {
			s += "_"
		}
		// determine next to append to r
		var next string
		if ism := ini.Peek(r[i:]); ism != "" && (!lastWasUpper || lastWasIsm) {
			next = ism
		} else {
			next = string(r[i])
		}
		// save for next iteration
		lastWasIsm, lastWasUpper, lastWasLetter = len(next) > 1, isUpper, isLetter
		s += next
		i += len(next)
	}
	return strings.ToLower(s)
}

// CamelToSnakeIdentifier converts name from camel case to a snake case
// identifier.
func (ini *Initialisms) CamelToSnakeIdentifier(name string) string {
	return ToIdentifier(ini.CamelToSnake(name))
}

// SnakeToCamel converts name to CamelCase.
func (ini *Initialisms) SnakeToCamel(name string) string {
	var s string
	for _, word := range strings.Split(name, "_") {
		if word == "" {
			continue
		}
		u := strings.ToUpper(word)
		if ok := ini.m[u]; ok {
			s += u
		} else {
			s += strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	return s
}

// SnakeToCamelIdentifier converts name to its CamelCase identifier (first
// letter is capitalized).
func (ini *Initialisms) SnakeToCamelIdentifier(name string) string {
	return ini.SnakeToCamel(ToIdentifier(name))
}

// ForceCamelIdentifier forces name to its CamelCase specific to Go
// ("AnIdentifier").
func (ini *Initialisms) ForceCamelIdentifier(name string) string {
	if name == "" {
		return ""
	}
	return ini.SnakeToCamelIdentifier(ini.CamelToSnake(name))
}

// ForceLowerCamelIdentifier forces the first portion of an identifier to be
// lower case ("anIdentifier").
func (ini *Initialisms) ForceLowerCamelIdentifier(name string) string {
	if name == "" {
		return ""
	}
	name = ini.CamelToSnake(name)
	first := strings.SplitN(name, "_", -1)[0]
	name = ini.SnakeToCamelIdentifier(name)
	return strings.ToLower(first) + name[len(first):]
}

// Peek returns the next longest possible initialism in r.
func (ini *Initialisms) Peek(r []rune) string {
	// do no work
	if len(r) < 2 {
		return ""
	}
	// grab at most next maxInitialismLen uppercase characters
	l := min(len(r), ini.max)
	var z []rune
	for i := 0; i < l; i++ {
		if !unicode.IsUpper(r[i]) {
			break
		}
		z = append(z, r[i])
	}
	// bail if next few characters were not uppercase.
	if len(z) < 2 {
		return ""
	}
	// determine if common initialism
	for i := min(ini.max, len(z)); i >= 2; i-- {
		if r := string(z[:i]); ini.m[r] {
			return r
		}
	}
	return ""
}

// IsInitialism indicates whether or not s is a registered initialism.
func (ini *Initialisms) IsInitialism(s string) bool {
	return ini.m[strings.ToUpper(s)]
}

// min returns the minimum of a, b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the max of a, b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
