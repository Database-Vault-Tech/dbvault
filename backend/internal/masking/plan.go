package masking

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

// Plan is a profile resolved against the schema actually restored in the
// sandbox: exactly what will be changed.
type Plan struct {
	Tables []PlanTable
	// Key is the organization's masking key (hex); fakes are derived from it.
	Key string
}

type PlanTable struct {
	Table    engine.CatalogTable
	Truncate bool
	Columns  []PlanColumn
}

type PlanColumn struct {
	Column engine.CatalogColumn
	Rule   string
	Value  string
}

// Problems are reasons a plan can't run. Every one names the table/column.
type Problems []string

func (p Problems) Error() string {
	if len(p) == 1 {
		return "masking can't run: " + p[0]
	}
	return fmt.Sprintf("masking can't run (%d problems): %s", len(p), strings.Join(p, "; "))
}

// Build resolves rules against a catalog. It fails closed: a column that
// looks personal and has no rule (schema drift, e.g. a newly added
// recovery_email) is a problem unless the rules mark it "keep".
func Build(rules Rules, cat engine.Catalog, key string) (Plan, error) {
	problems := Problems(rules.Validate())
	byKey := map[string]engine.CatalogTable{}
	for _, t := range cat.Tables {
		byKey[tableKey(t)] = t
	}
	truncated := map[string]bool{}
	for name, tr := range rules.Tables {
		if t, ok := byKey[name]; ok && tr.Truncate {
			truncated[t.Table().String()] = true
		}
	}

	plan := Plan{Key: key}
	for _, name := range sortedKeys(rules.Tables) {
		tr := rules.Tables[name]
		t, ok := byKey[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("table %s doesn't exist in this backup", name))
			continue
		}
		if tr.Truncate {
			// Emptying a table other tables point at would break their
			// foreign keys when the masked copy is restored.
			for _, other := range cat.Tables {
				if truncated[other.Table().String()] {
					continue
				}
				for _, ref := range other.References {
					if ref == t.Table().String() {
						problems = append(problems, fmt.Sprintf("%s can't be truncated because %s references it; truncate %s too, or mask %s's columns instead",
							name, tableKey(other), tableKey(other), name))
					}
				}
			}
			plan.Tables = append(plan.Tables, PlanTable{Table: t, Truncate: true})
			continue
		}
		cols := map[string]engine.CatalogColumn{}
		for _, c := range t.Columns {
			cols[c.Name] = c
		}
		pt := PlanTable{Table: t}
		for _, cname := range sortedKeys(tr.Columns) {
			rule := tr.Columns[cname]
			c, ok := cols[cname]
			if !ok {
				problems = append(problems, fmt.Sprintf("column %s.%s doesn't exist in this backup", name, cname))
				continue
			}
			if msg := checkColumn(c, rule); msg != "" {
				problems = append(problems, fmt.Sprintf("%s.%s: %s", name, cname, msg))
				continue
			}
			if rule.Rule != RuleKeep {
				pt.Columns = append(pt.Columns, PlanColumn{Column: c, Rule: rule.Rule, Value: rule.Value})
			}
		}
		if len(pt.Columns) > 0 {
			plan.Tables = append(plan.Tables, pt)
		}
	}

	// Drift: personal-looking columns nobody decided about.
	for _, t := range cat.Tables {
		tr := rules.Tables[tableKey(t)]
		if tr.Truncate {
			continue
		}
		for _, c := range t.Columns {
			if _, ruled := tr.Columns[c.Name]; ruled {
				continue
			}
			if SuggestColumn(c) != "" {
				problems = append(problems, fmt.Sprintf("%s.%s looks like personal data but has no rule; add one, or mark it \"keep\"", tableKey(t), c.Name))
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return plan, problems
	}
	return plan, nil
}

// checkColumn returns why rule can't apply to c, or "".
func checkColumn(c engine.CatalogColumn, rule ColumnRule) string {
	if rule.Rule == RuleKeep {
		return ""
	}
	if c.Key {
		return "primary- and foreign-key columns can't be masked"
	}
	if c.Unique && !keepsUnique(rule.Rule) {
		return fmt.Sprintf("the column is unique, and %s can produce duplicates; use email, hash or null", rule.Rule)
	}
	switch rule.Rule {
	case RuleNull:
		if !c.Nullable {
			return "the column is NOT NULL, so it can't be set to null"
		}
	case RuleDateShift:
		if !isDateType(c.Type) {
			return fmt.Sprintf("date_shift needs a date or timestamp column (this is %s)", c.Type)
		}
	default:
		if !isTextType(c.Type) {
			return fmt.Sprintf("%s writes text, but the column is %s; use null or date_shift", rule.Rule, c.Type)
		}
		if rule.Rule == RuleRedact && c.MaxLength > 0 && len([]rune(rule.Value)) > c.MaxLength {
			return fmt.Sprintf("the redact value is longer than the column (%d characters)", c.MaxLength)
		}
	}
	return ""
}

// Masker is implemented by drivers that can mask a sandbox database.
type Masker interface {
	engine.Cataloger
	// Mask applies the plan to dbName on t's server, with triggers off, and
	// then runs the post-masking checks.
	Mask(ctx context.Context, t engine.Target, dbName string, plan Plan, log engine.Logger) (Report, error)
}

// Report summarizes a masking run. It never contains data values.
type Report struct {
	ProfileVersion int           `json:"profile_version"`
	Tables         []TableReport `json:"tables"`
	RowsChanged    int64         `json:"rows_changed"`
	Checks         int           `json:"checks_passed"`
	DurationMs     int64         `json:"duration_ms"`
}

type TableReport struct {
	Table   string   `json:"table"`
	Action  string   `json:"action"` // "masked" or "truncated"
	Rows    int64    `json:"rows"`
	Columns []string `json:"columns,omitempty"` // "email: email"
}

// Checks lists the post-masking assertions for a plan, as engine-neutral
// SQL fragments: each counts rows that would mean masking failed.
type Check struct {
	Table       engine.CatalogTable
	Description string
	// Column is the column checked; empty for "empty" checks.
	Column string
	Kind   string // "empty", "email", "equals", "null"
	Value  string
}

func Checks(p Plan) []Check {
	var out []Check
	for _, t := range p.Tables {
		if t.Truncate {
			out = append(out, Check{Table: t.Table, Kind: "empty", Description: tableKey(t.Table) + " is empty"})
			continue
		}
		for _, c := range t.Columns {
			switch c.Rule {
			case RuleEmail:
				out = append(out, Check{Table: t.Table, Column: c.Column.Name, Kind: "email", Description: tableKey(t.Table) + "." + c.Column.Name + " holds only generated emails"})
			case RuleRedact:
				out = append(out, Check{Table: t.Table, Column: c.Column.Name, Kind: "equals", Value: c.Value, Description: tableKey(t.Table) + "." + c.Column.Name + " is redacted"})
			case RuleNull:
				out = append(out, Check{Table: t.Table, Column: c.Column.Name, Kind: "null", Description: tableKey(t.Table) + "." + c.Column.Name + " is empty"})
			}
		}
	}
	return out
}

// CheckSQL renders a check as a count query over an already-quoted table
// and column. "equals" checks take one parameter, written as placeholder
// ("$1" for PostgreSQL, "?" for SQLite).
func CheckSQL(c Check, table, column, placeholder string) string {
	switch c.Kind {
	case "empty":
		return "SELECT count(*) FROM " + table
	case "email":
		return "SELECT count(*) FROM " + table + " WHERE " + column + " IS NOT NULL AND " + column + " NOT LIKE 'user!_%@example.com' ESCAPE '!'"
	case "equals":
		return "SELECT count(*) FROM " + table + " WHERE " + column + " IS NOT NULL AND " + column + " <> " + placeholder
	default: // null
		return "SELECT count(*) FROM " + table + " WHERE " + column + " IS NOT NULL"
	}
}
