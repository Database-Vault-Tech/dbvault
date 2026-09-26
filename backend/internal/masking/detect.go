package masking

import (
	"regexp"
	"strings"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

// columnPatterns map column names that usually hold personal data to the rule
// suggested for them. Order matters: the first match wins.
var columnPatterns = []struct {
	re   *regexp.Regexp
	rule string
}{
	{regexp.MustCompile(`(^|_)(password|passwd|pwd|secret|token|api_?key|otp|totp)(_|$)|_hash$|^hash$`), RuleRedact},
	{regexp.MustCompile(`(^|_)e?mail(_address)?(_|$)`), RuleEmail},
	{regexp.MustCompile(`^(first_?name|given_?name|forename)$`), RuleFirstName},
	{regexp.MustCompile(`^(last_?name|surname|family_?name)$`), RuleLastName},
	{regexp.MustCompile(`^(full_?name|display_?name|name|contact_?name|customer_?name|user_?name)$`), RuleName},
	{regexp.MustCompile(`(^|_)(phone|mobile|cell|telephone|tel|fax)(_|$)`), RulePhone},
	{regexp.MustCompile(`(^|_)(ssn|social_security|national_id|passport|tax_?id|driver_?licen[cs]e|nin)(_|$)`), RuleHash},
	{regexp.MustCompile(`(^|_)(ip|ip_?address|last_ip|remote_addr)(_|$)`), RuleHash},
	{regexp.MustCompile(`(^|_)(card|pan|iban|account_?number|routing_?number|cvv|cvc)(_|$)`), RuleRedact},
	{regexp.MustCompile(`(^|_)(address|street|address_?line|postcode|postal_?code|zip|zip_?code)(_|\d|$)`), RuleRedact},
	{regexp.MustCompile(`^(dob|birth_?date|date_of_birth|birthday)$`), RuleDateShift},
}

// tablePatterns are tables usually safe (and much faster) to empty.
var tablePatterns = regexp.MustCompile(`^(sessions?|.*_sessions?|.*_logs?|logs?|audit.*|.*_audit|events?|.*_events|activity.*|.*_activities|password_resets?|.*_tokens?)$`)

// SuggestColumn returns the rule suggested for a column, or "" when it
// doesn't look personal. Key columns are never suggested.
func SuggestColumn(c engine.CatalogColumn) string {
	if c.Key {
		return ""
	}
	name := strings.ToLower(c.Name)
	for _, p := range columnPatterns {
		if !p.re.MatchString(name) {
			continue
		}
		rule := p.rule
		// Fall back to rules the column's type can take.
		switch {
		case rule == RuleDateShift && !isDateType(c.Type):
			rule = RuleRedact
		case rule != RuleDateShift && !isTextType(c.Type):
			if c.Nullable {
				rule = RuleNull
			} else {
				continue
			}
		}
		if rule == RuleRedact && !isTextType(c.Type) {
			rule = RuleNull
		}
		// A unique column needs a rule that keeps values distinct.
		if c.Unique && !keepsUnique(rule) {
			rule = RuleHash
			if !isTextType(c.Type) {
				continue
			}
		}
		return rule
	}
	return ""
}

// SuggestTable reports whether a table looks like logs/sessions/events that
// are best emptied.
func SuggestTable(t engine.CatalogTable) bool {
	return tablePatterns.MatchString(strings.ToLower(t.Name))
}

// Suggest proposes rules for every personal-looking column in a catalog.
// Tables that look like logs are truncated when nothing references them.
func Suggest(cat engine.Catalog) Rules {
	rules := Rules{Tables: map[string]TableRule{}}
	referenced := map[string]bool{}
	for _, t := range cat.Tables {
		for _, ref := range t.References {
			if ref != t.Table().String() {
				referenced[ref] = true
			}
		}
	}
	for _, t := range cat.Tables {
		key := tableKey(t)
		if SuggestTable(t) && !referenced[t.Table().String()] {
			rules.Tables[key] = TableRule{Truncate: true}
			continue
		}
		cols := map[string]ColumnRule{}
		for _, c := range t.Columns {
			if rule := SuggestColumn(c); rule != "" {
				cr := ColumnRule{Rule: rule}
				if rule == RuleRedact {
					cr.Value = redactValue(c)
				}
				cols[c.Name] = cr
			}
		}
		if len(cols) > 0 {
			rules.Tables[key] = TableRule{Columns: cols}
		}
	}
	return rules
}

func redactValue(c engine.CatalogColumn) string {
	v := "REDACTED"
	if c.MaxLength > 0 && c.MaxLength < len(v) {
		v = v[:c.MaxLength]
	}
	return v
}

// tableKey is how a table is named in rules: without the default schema.
func tableKey(t engine.CatalogTable) string {
	if t.Schema == "" || t.Schema == "public" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

func keepsUnique(rule string) bool {
	return rule == RuleEmail || rule == RuleHash || rule == RuleNull || rule == RuleKeep
}

// textTypes matches character types across engines: text, citext,
// character varying(n), varchar, bpchar, nvarchar, clob, longtext…
var textTypes = regexp.MustCompile(`(?i)char|text|clob|string`)

// isTextType reports whether masking can write strings into a column. SQLite
// columns with no declared type accept anything.
func isTextType(t string) bool {
	t = strings.TrimSpace(t)
	return t == "" || textTypes.MatchString(t)
}

func isDateType(t string) bool {
	t = strings.ToLower(t)
	return strings.Contains(t, "date") || strings.Contains(t, "timestamp") || strings.Contains(t, "time")
}
