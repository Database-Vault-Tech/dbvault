// Package validate provides small, explicit request validation helpers.
package validate

import (
	"net"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dbvault/dbvault/backend/internal/apperr"
)

type V struct {
	fields map[string]string
}

func New() *V { return &V{fields: map[string]string{}} }

// Check records msg for field when ok is false (first error per field wins).
func (v *V) Check(ok bool, field, msg string) {
	if !ok {
		if _, exists := v.fields[field]; !exists {
			v.fields[field] = msg
		}
	}
}

func (v *V) Required(field, value string) {
	v.Check(strings.TrimSpace(value) != "", field, "This field is required.")
}

func (v *V) MaxLen(field, value string, n int) {
	v.Check(utf8.RuneCountInString(value) <= n, field, "Must be at most "+strconv.Itoa(n)+" characters.")
}

func (v *V) MinLen(field, value string, n int) {
	v.Check(utf8.RuneCountInString(value) >= n, field, "Must be at least "+strconv.Itoa(n)+" characters.")
}

func (v *V) OneOf(field, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	v.Check(false, field, "Must be one of: "+strings.Join(allowed, ", ")+".")
}

func (v *V) Email(field, value string) {
	addr, err := mail.ParseAddress(value)
	v.Check(err == nil && addr.Address == value && len(value) <= 254, field, "Must be a valid email address.")
}

func (v *V) Range(field string, value, min, max int) {
	v.Check(value >= min && value <= max, field, "Must be between "+strconv.Itoa(min)+" and "+strconv.Itoa(max)+".")
}

func (v *V) Has(field string) bool {
	_, ok := v.fields[field]
	return ok
}

// Err returns a validation error when any check failed.
func (v *V) Err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return apperr.Validation(v.fields)
}

var hostnameRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-_]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-_]{0,61}[a-zA-Z0-9])?)*\.?$`)

// IsHost reports whether s is a hostname or IP address (no ports, paths or sockets).
func IsHost(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	if net.ParseIP(strings.Trim(s, "[]")) != nil {
		return true
	}
	return hostnameRE.MatchString(s)
}

var identRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]{0,62}$`)

// IsPGIdentifier reports whether s is a conservative PostgreSQL identifier
// (used for database names we create ourselves).
func IsPGIdentifier(s string) bool { return identRE.MatchString(s) }

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _.\-]{0,62}$`)

// IsResourceName reports whether s is a valid human-facing resource name.
func IsResourceName(s string) bool { return nameRE.MatchString(s) }

// relPathRE allows the characters people use in file and folder names,
// separated by forward slashes.
var relPathRE = regexp.MustCompile(`^[A-Za-z0-9._\- +@()]+(/[A-Za-z0-9._\- +@()]+)*$`)

// IsRelativeFilePath reports whether s is a safe path to a file inside a
// configured directory: relative, forward slashes, no "." or ".." segments,
// no hidden leading dash, at most 255 bytes.
func IsRelativeFilePath(s string) bool {
	if s == "" || len(s) > 255 || !relPathRE.MatchString(s) {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "." || seg == ".." || strings.HasPrefix(seg, "-") || strings.TrimSpace(seg) != seg {
			return false
		}
	}
	return true
}
