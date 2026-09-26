// Package masking turns a production backup into a safe copy for staging and
// development: it validates masking rules against a database's schema,
// suggests rules for columns that look personal, and generates the same
// deterministic fake values every engine produces. Engine drivers apply the
// rules inside a restore sandbox (see Masker); real values never leave it.
package masking

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Rule names.
const (
	RuleEmail     = "email"
	RuleName      = "name"
	RuleFirstName = "first_name"
	RuleLastName  = "last_name"
	RulePhone     = "phone"
	RuleHash      = "hash"
	RuleDateShift = "date_shift"
	RuleRedact    = "redact"
	RuleNull      = "null"
	// RuleKeep copies a column unchanged, and records that someone decided
	// a personal-looking column is fine to keep.
	RuleKeep = "keep"
)

var validRules = map[string]bool{
	RuleEmail: true, RuleName: true, RuleFirstName: true, RuleLastName: true, RulePhone: true,
	RuleHash: true, RuleDateShift: true, RuleRedact: true, RuleNull: true, RuleKeep: true,
}

// Rules is a masking profile's content. In JSON (and the CLI's YAML) it uses
// the compact form:
//
//	{"tables": {"users": {"email": "email", "password_hash": {"redact": "masked"}},
//	            "payments": "truncate"}}
//
// Table keys are "name" (the default schema) or "schema.name".
type Rules struct {
	Tables map[string]TableRule
}

// TableRule either truncates a table or masks some of its columns.
type TableRule struct {
	Truncate bool
	Columns  map[string]ColumnRule
}

// ColumnRule is one column's rule. Value is the constant for RuleRedact.
type ColumnRule struct {
	Rule  string
	Value string
}

func (r Rules) MarshalJSON() ([]byte, error) {
	tables := map[string]any{}
	for name, t := range r.Tables {
		if t.Truncate {
			tables[name] = "truncate"
			continue
		}
		cols := map[string]any{}
		for c, rule := range t.Columns {
			if rule.Rule == RuleRedact {
				cols[c] = map[string]string{RuleRedact: rule.Value}
			} else {
				cols[c] = rule.Rule
			}
		}
		tables[name] = cols
	}
	return json.Marshal(map[string]any{"tables": tables})
}

func (r *Rules) UnmarshalJSON(b []byte) error {
	var raw struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	r.Tables = map[string]TableRule{}
	for name, v := range raw.Tables {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if s != "truncate" {
				return fmt.Errorf("table %q: use \"truncate\" or a map of column rules", name)
			}
			r.Tables[name] = TableRule{Truncate: true}
			continue
		}
		var cols map[string]json.RawMessage
		if err := json.Unmarshal(v, &cols); err != nil {
			return fmt.Errorf("table %q: use \"truncate\" or a map of column rules", name)
		}
		t := TableRule{Columns: map[string]ColumnRule{}}
		for c, cv := range cols {
			var rule string
			if json.Unmarshal(cv, &rule) == nil {
				t.Columns[c] = ColumnRule{Rule: rule}
				continue
			}
			var obj map[string]string
			if err := json.Unmarshal(cv, &obj); err != nil || len(obj) != 1 {
				return fmt.Errorf("%s.%s: use a rule name, or {\"redact\": \"value\"}", name, c)
			}
			val, ok := obj[RuleRedact]
			if !ok {
				return fmt.Errorf("%s.%s: only redact takes a value", name, c)
			}
			t.Columns[c] = ColumnRule{Rule: RuleRedact, Value: val}
		}
		r.Tables[name] = t
	}
	return nil
}

// Validate checks the rules on their own (without a schema).
func (r Rules) Validate() []string {
	var problems []string
	for _, name := range sortedKeys(r.Tables) {
		t := r.Tables[name]
		if strings.TrimSpace(name) == "" || strings.Count(name, ".") > 1 {
			problems = append(problems, fmt.Sprintf("%q is not a table name", name))
		}
		for _, c := range sortedKeys(t.Columns) {
			rule := t.Columns[c]
			if !validRules[rule.Rule] {
				problems = append(problems, fmt.Sprintf("%s.%s: unknown rule %q", name, c, rule.Rule))
			}
			if rule.Rule == RuleRedact && len(rule.Value) > 1024 {
				problems = append(problems, fmt.Sprintf("%s.%s: the redact value is too long", name, c))
			}
		}
	}
	return problems
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
