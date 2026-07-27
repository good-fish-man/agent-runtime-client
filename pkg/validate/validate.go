// Package validate offers a tiny wrapper over go-playground/validator using the
// `validate` struct tag, matching agent-frame's DTO annotations so request DTOs
// can be reused verbatim.
package validate

import "github.com/go-playground/validator/v10"

var v = validator.New()

// Struct validates s against its `validate` tags, returning the first error.
func Struct(s any) error {
	return v.Struct(s)
}
