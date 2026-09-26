package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	msqlite "modernc.org/sqlite"

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/masking"
)

var (
	_ engine.Cataloger = (*Driver)(nil)
	_ masking.Masker   = (*Driver)(nil)
)

var registerOnce sync.Once

// registerMaskFunction makes dbvault_mask(rule, key, value, max_len)
// available on new connections. It runs masking.Fake, so SQLite produces
// exactly the fakes PostgreSQL computes in SQL.
func registerMaskFunction() {
	registerOnce.Do(func() {
		msqlite.MustRegisterDeterministicScalarFunction("dbvault_mask", 4, func(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			rule, _ := args[0].(string)
			key, _ := args[1].(string)
			maxLen, _ := args[3].(int64)
			if args[2] == nil {
				return nil, nil
			}
			if rule == masking.RuleDateShift {
				return shiftDate(key, args[2])
			}
			return masking.Fake(rule, key, valueText(args[2]), int(maxLen))
		})
	})
}

// valueText is how a stored value is hashed: its text form, as PostgreSQL's
// ::text would print it.
func valueText(v driver.Value) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	}
	return fmt.Sprint(v)
}

// shiftDate moves a date by masking.ShiftDays. SQLite stores dates as text,
// Unix seconds (integer) or Julian days (real).
func shiftDate(key string, v driver.Value) (driver.Value, error) {
	text := valueText(v)
	days := masking.ShiftDays(key, text)
	switch x := v.(type) {
	case int64:
		return x + int64(days)*86400, nil
	case float64:
		return x + float64(days), nil
	case time.Time:
		return x.AddDate(0, 0, days), nil
	}
	return masking.ShiftDateText(key, text)
}

var varcharLen = regexp.MustCompile(`\((\d+)\)`)

// Catalog reads tables, columns, keys and unique indexes with PRAGMAs.
func (d *Driver) Catalog(ctx context.Context, t engine.Target, dbName string) (engine.Catalog, error) {
	var cat engine.Catalog
	p, err := d.resolve(t, dbName)
	if err != nil {
		return cat, err
	}
	db, err := open(p, true)
	if err != nil {
		return cat, err
	}
	defer db.Close()
	names, err := tableNames(ctx, db)
	if err != nil {
		return cat, friendly(dbName, err)
	}
	for _, name := range names {
		tbl := engine.CatalogTable{Name: name}
		keys := map[string]bool{}
		unique := map[string]bool{}

		fks, err := db.QueryContext(ctx, `SELECT "table", "from" FROM pragma_foreign_key_list(?)`, name)
		if err != nil {
			return cat, err
		}
		refs := map[string]bool{}
		for fks.Next() {
			var ref, from string
			if err := fks.Scan(&ref, &from); err != nil {
				fks.Close()
				return cat, err
			}
			keys[from] = true
			refs[ref] = true
		}
		fks.Close()
		for ref := range refs {
			tbl.References = append(tbl.References, ref)
		}

		idx, err := db.QueryContext(ctx, `SELECT ii.name FROM pragma_index_list(?) il, pragma_index_info(il.name) ii WHERE il."unique" = 1`, name)
		if err != nil {
			return cat, err
		}
		for idx.Next() {
			var col sql.NullString
			if err := idx.Scan(&col); err != nil {
				idx.Close()
				return cat, err
			}
			if col.Valid {
				unique[col.String] = true
			}
		}
		idx.Close()

		cols, err := db.QueryContext(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info(?)`, name)
		if err != nil {
			return cat, err
		}
		for cols.Next() {
			var c engine.CatalogColumn
			var notNull, pk int
			if err := cols.Scan(&c.Name, &c.Type, &notNull, &pk); err != nil {
				cols.Close()
				return cat, err
			}
			c.Key = pk > 0 || keys[c.Name]
			c.Nullable = notNull == 0 && pk == 0
			c.Unique = unique[c.Name] || pk > 0
			if m := varcharLen.FindStringSubmatch(c.Type); m != nil && strings.Contains(strings.ToUpper(c.Type), "CHAR") {
				c.MaxLength, _ = strconv.Atoi(m[1])
			}
			tbl.Columns = append(tbl.Columns, c)
		}
		cols.Close()
		cat.Tables = append(cat.Tables, tbl)
	}
	return cat, nil
}

// Mask applies a plan in one transaction. SQLite has no switch to turn
// triggers off, so they are dropped and recreated from their stored SQL
// inside the same transaction. The masked file is later copied with VACUUM
// INTO, which leaves no freed pages holding the original values.
func (d *Driver) Mask(ctx context.Context, t engine.Target, dbName string, plan masking.Plan, log engine.Logger) (masking.Report, error) {
	start := time.Now()
	var rep masking.Report
	registerMaskFunction()
	p, err := d.resolve(t, dbName)
	if err != nil {
		return rep, err
	}
	db, err := open(p, false)
	if err != nil {
		return rep, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return rep, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()

	type trigger struct{ name, sql string }
	var triggers []trigger
	rows, err := tx.QueryContext(ctx, `SELECT name, sql FROM sqlite_schema WHERE type = 'trigger' AND sql IS NOT NULL`)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var tr trigger
		if err := rows.Scan(&tr.name, &tr.sql); err != nil {
			rows.Close()
			return rep, err
		}
		triggers = append(triggers, tr)
	}
	rows.Close()
	for _, tr := range triggers {
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+quoteIdent(tr.name)); err != nil {
			return rep, err
		}
	}

	for _, pt := range plan.Tables {
		name := quoteIdent(pt.Table.Name)
		if pt.Truncate {
			res, err := tx.ExecContext(ctx, "DELETE FROM "+name)
			if err != nil {
				return rep, fmt.Errorf("truncating %s: %w", pt.Table.Name, err)
			}
			n, _ := res.RowsAffected()
			rep.Tables = append(rep.Tables, masking.TableReport{Table: pt.Table.Name, Action: "truncated", Rows: n})
			rep.RowsChanged += n
			log.Infof("Truncated %s (%d rows)", pt.Table.Name, n)
			continue
		}
		var sets, cols []string
		var args []any
		for _, c := range pt.Columns {
			col := quoteIdent(c.Column.Name)
			switch c.Rule {
			case masking.RuleNull:
				sets = append(sets, col+" = NULL")
			case masking.RuleRedact:
				args = append(args, c.Value)
				sets = append(sets, col+" = CASE WHEN "+col+" IS NULL THEN NULL ELSE ? END")
			default:
				args = append(args, c.Rule, plan.Key, c.Column.MaxLength)
				sets = append(sets, col+" = dbvault_mask(?, ?, "+col+", ?)")
			}
			cols = append(cols, c.Column.Name+": "+c.Rule)
		}
		res, err := tx.ExecContext(ctx, "UPDATE "+name+" SET "+strings.Join(sets, ", "), args...)
		if err != nil {
			return rep, fmt.Errorf("masking %s: %w", pt.Table.Name, err)
		}
		n, _ := res.RowsAffected()
		rep.Tables = append(rep.Tables, masking.TableReport{Table: pt.Table.Name, Action: "masked", Rows: n, Columns: cols})
		rep.RowsChanged += n
		log.Infof("Masked %s: %d rows (%s)", pt.Table.Name, n, strings.Join(cols, ", "))
	}

	for _, tr := range triggers {
		if _, err := tx.ExecContext(ctx, tr.sql); err != nil {
			return rep, fmt.Errorf("recreating trigger %s: %w", tr.name, err)
		}
	}
	for _, c := range masking.Checks(plan) {
		q := masking.CheckSQL(c, quoteIdent(c.Table.Name), quoteIdent(c.Column), "?")
		var args []any
		if c.Kind == "equals" {
			args = []any{c.Value}
		}
		var bad int64
		if err := tx.QueryRowContext(ctx, q, args...).Scan(&bad); err != nil {
			return rep, fmt.Errorf("checking that %s: %w", c.Description, err)
		}
		if bad > 0 {
			return rep, fmt.Errorf("masking check failed: expected %s, but %d rows don't match", c.Description, bad)
		}
		rep.Checks++
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	rep.DurationMs = time.Since(start).Milliseconds()
	return rep, nil
}
