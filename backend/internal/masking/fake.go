package masking

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NewKey returns a random masking key (hex). Each organization has one,
// sealed with ENCRYPTION_KEY; without it a fake can't be linked back to the
// original by hashing guesses.
func NewKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// digest is hex(SHA-256(key || value)). Every engine computes exactly this
// (PostgreSQL in SQL, SQLite through a Go function), so a value gets the
// same fake everywhere.
func digest(key, value string) string {
	sum := sha256.Sum256([]byte(key + value))
	return hex.EncodeToString(sum[:])
}

// n1 and n2 are two independent numbers taken from a digest, the same way
// the SQL expressions do: the first and second 7 hex digits.
func n1(h string) int64 { v, _ := strconv.ParseInt(h[0:7], 16, 64); return v }
func n2(h string) int64 { v, _ := strconv.ParseInt(h[7:14], 16, 64); return v }

// Fake returns the masked value for a text rule. maxLen (0 = unlimited)
// shortens hashes to fit the column.
func Fake(rule, key, value string, maxLen int) (string, error) {
	h := digest(key, value)
	switch rule {
	case RuleEmail:
		return "user_" + h[:10] + "@example.com", nil
	case RuleFirstName:
		return FirstNames[n1(h)%int64(len(FirstNames))], nil
	case RuleLastName:
		return LastNames[n2(h)%int64(len(LastNames))], nil
	case RuleName:
		return FirstNames[n1(h)%int64(len(FirstNames))] + " " + LastNames[n2(h)%int64(len(LastNames))], nil
	case RulePhone:
		return fmt.Sprintf("+1 555 %03d %04d", n1(h)%1000, n2(h)%10000), nil
	case RuleHash:
		if maxLen > 0 && maxLen < len(h) {
			return h[:maxLen], nil
		}
		return h, nil
	}
	return "", fmt.Errorf("rule %q has no generated value", rule)
}

// ShiftDays is how far date_shift moves a value: -180..180 days.
func ShiftDays(key, value string) int { return int(n1(digest(key, value))%361) - 180 }

// dateLayouts are the textual date formats SQLite databases commonly store.
var dateLayouts = []string{
	"2006-01-02",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05.999999999",
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
}

// ShiftDateText shifts a textual date, keeping its format. Values it can't
// parse are an error: silently keeping a real birth date would leak it.
func ShiftDateText(key, value string) (string, error) {
	v := strings.TrimSpace(value)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.AddDate(0, 0, ShiftDays(key, value)).Format(layout), nil
		}
	}
	return "", fmt.Errorf("%q isn't a date date_shift understands (use YYYY-MM-DD or YYYY-MM-DD HH:MM:SS)", truncateForError(value))
}

func truncateForError(s string) string {
	// Errors must not echo personal data; show only the shape.
	var b strings.Builder
	for i, r := range s {
		if i >= 12 {
			b.WriteString("…")
			break
		}
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune('9')
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			b.WriteRune('x')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Examples shows each rule on made-up input for the profile editor: the UI
// never shows real rows.
func Examples(key string) map[string]string {
	out := map[string]string{}
	for rule, input := range map[string]string{
		RuleEmail: "jane.doe@company.com", RuleName: "Jane Doe", RuleFirstName: "Jane", RuleLastName: "Doe",
		RulePhone: "+1 202 555 0147", RuleHash: "AB123456C",
	} {
		v, _ := Fake(rule, key, input, 16)
		out[rule] = input + " → " + v
	}
	d, _ := ShiftDateText(key, "1990-12-10")
	out[RuleDateShift] = "1990-12-10 → " + d
	out[RuleRedact] = "4111 1111 1111 1111 → REDACTED"
	out[RuleNull] = "10 Downing St → NULL"
	out[RuleKeep] = "unchanged"
	return out
}
