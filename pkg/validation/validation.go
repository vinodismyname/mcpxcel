package validation

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/vinodismyname/mcpxcel/pkg/pagination"
)

var (
	v          *validator.Validate
	onceCursor = regexp.MustCompile(`^[A-Za-z0-9_\-\.]+$`)
)

// Validator returns a singleton validator with custom rules registered.
func Validator() *validator.Validate {
	if v == nil {
		v = validator.New()
		// Custom: Excel file path must have supported extension
		_ = v.RegisterValidation("filepath_ext", func(fl validator.FieldLevel) bool {
			s := strings.TrimSpace(fl.Field().String())
			if s == "" {
				return false
			}
			s = strings.ToLower(s)
			return strings.HasSuffix(s, ".xlsx") || strings.HasSuffix(s, ".xlsm") || strings.HasSuffix(s, ".xltx") || strings.HasSuffix(s, ".xltm")
		})
		// Custom: A1-style range or a plausible defined name
		_ = v.RegisterValidation("a1orname", func(fl validator.FieldLevel) bool {
			s := strings.TrimSpace(fl.Field().String())
			if s == "" {
				return false
			}
			// A1:A1 style
			if strings.Contains(s, ":") {
				parts := strings.Split(s, ":")
				if len(parts) != 2 {
					return false
				}
				a1 := regexp.MustCompile(`^[A-Za-z]+[0-9]+$`)
				return a1.MatchString(parts[0]) && a1.MatchString(parts[1])
			}
			// Named range heuristic: letters, numbers, underscore, dot, space
			nameRe := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_\. ]{0,63}$`)
			return nameRe.MatchString(s)
		})
		// Custom: cursor must be decodable via pagination.DecodeCursor
		_ = v.RegisterValidation("cursor", func(fl validator.FieldLevel) bool {
			s := strings.TrimSpace(fl.Field().String())
			if s == "" {
				return true // empty is allowed; use omitempty with this tag
			}
			// Quick URL-safe base64 precheck
			if _, err := base64.RawURLEncoding.DecodeString(s); err != nil {
				return false
			}
			if _, err := pagination.DecodeCursor(s); err != nil {
				return false
			}
			return true
		})
		// Custom: valid_regex – only enforced if a sibling boolean field named "Regex" is true
		_ = v.RegisterValidation("valid_regex", func(fl validator.FieldLevel) bool {
			// Only enforce when parent has Regex=true
			parent := fl.Parent()
			if parent.IsValid() {
				rf := parent.FieldByName("Regex")
				if rf.IsValid() && rf.Kind().String() == "bool" && rf.Bool() {
					s := fl.Field().String()
					if s == "" {
						return false
					}
					// Defer actual compile check to handler if needed; basic sanity
					// Accept anything non-empty; runtime will compile and return detailed error
					return true
				}
			}
			return true
		})
	}
	return v
}

// ValidateStruct validates a struct and returns a user-friendly error string
// suitable for MCP tool errors. Returns empty string when valid.
func ValidateStruct(s any) string {
	if err := Validator().Struct(s); err != nil {
		if ve, ok := err.(validator.ValidationErrors); ok && len(ve) > 0 {
			fe := ve[0]
			field := strings.ToLower(fe.Field())
			switch fe.Tag() {
			case "required":
				return fmt.Sprintf("VALIDATION: %s is required", field)
			case "required_without":
				// Common pattern: sheet/query/predicate required unless cursor provided
				if field == "sheet" {
					return "VALIDATION: sheet is required (or supply cursor)"
				}
				if field == "query" {
					return "VALIDATION: query is required (or supply cursor)"
				}
				if field == "predicate" {
					return "VALIDATION: predicate is required (or supply cursor)"
				}
				return fmt.Sprintf("VALIDATION: %s is required", field)
			case "filepath_ext":
				return "VALIDATION: path must be an Excel file (.xlsx, .xlsm, .xltx, .xltm)"
			case "a1orname":
				return "VALIDATION: invalid range; use A1:D50 or a defined name"
			case "cursor":
				return "CURSOR_INVALID: failed to decode cursor; reopen workbook and restart pagination"
			case "valid_regex":
				return "VALIDATION: invalid regex; examples: 'foo.*' or '^\\d{4}$'"
			case "min", "max", "gte", "lte":
				return fmt.Sprintf("VALIDATION: %s must satisfy %s=%s", field, fe.Tag(), fe.Param())
			}
			// Fallback generic
			return fmt.Sprintf("VALIDATION: invalid %s", field)
		}
		return "VALIDATION: invalid inputs"
	}
	return ""
}
