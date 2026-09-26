package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/masking"
)

var (
	_ engine.Cataloger = (*Driver)(nil)
	_ masking.Masker   = (*Driver)(nil)
)

func serverVersion(ctx context.Context, conn *pgx.Conn) (int, error) {
	var v string
	if err := conn.QueryRow(ctx, "SHOW server_version_num").Scan(&v); err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

// Catalog lists user tables, their columns and foreign keys. Partitions are
// left out: masking the partitioned parent covers them.
func (d *Driver) Catalog(ctx context.Context, t engine.Target, dbName string) (engine.Catalog, error) {
	var cat engine.Catalog
	conn, done, err := d.connect(ctx, t, dbName)
	if err != nil {
		return cat, err
	}
	defer done()
	version, err := serverVersion(ctx, conn)
	if err != nil {
		return cat, err
	}
	partitions := ""
	if version >= 100000 {
		partitions = " AND NOT c.relispartition"
	}
	rows, err := conn.Query(ctx, `SELECT n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype IN ('p', 'f') AND a.attnum = ANY (k.conkey)),
		EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = c.oid AND i.indisunique AND a.attnum = ANY (i.indkey::int2[])),
		CASE WHEN ty.typname IN ('varchar', 'bpchar') AND a.atttypmod > 4 THEN a.atttypmod - 4 ELSE 0 END
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		JOIN pg_type ty ON ty.oid = a.atttypid
		WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		AND n.nspname NOT LIKE 'pg\_toast%' AND n.nspname NOT LIKE 'pg\_temp%'`+partitions+`
		ORDER BY n.nspname, c.relname, a.attnum`)
	if err != nil {
		return cat, err
	}
	idx := map[string]int{}
	for rows.Next() {
		var schema, table string
		var col engine.CatalogColumn
		if err := rows.Scan(&schema, &table, &col.Name, &col.Type, &col.Nullable, &col.Key, &col.Unique, &col.MaxLength); err != nil {
			rows.Close()
			return cat, err
		}
		key := schema + "." + table
		i, ok := idx[key]
		if !ok {
			i = len(cat.Tables)
			idx[key] = i
			cat.Tables = append(cat.Tables, engine.CatalogTable{Schema: schema, Name: table})
		}
		cat.Tables[i].Columns = append(cat.Tables[i].Columns, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cat, err
	}
	refs, err := conn.Query(ctx, `SELECT DISTINCT n.nspname, c.relname, rn.nspname, rc.relname
		FROM pg_constraint k
		JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class rc ON rc.oid = k.confrelid JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		WHERE k.contype = 'f'`)
	if err != nil {
		return cat, err
	}
	defer refs.Close()
	for refs.Next() {
		var s, t, rs, rt string
		if err := refs.Scan(&s, &t, &rs, &rt); err != nil {
			return cat, err
		}
		if i, ok := idx[s+"."+t]; ok {
			cat.Tables[i].References = append(cat.Tables[i].References, rs+"."+rt)
		}
	}
	return cat, refs.Err()
}

// maskExpr is the SQL that computes one column's masked value. h is the
// keyed digest ($1 is the masking key); it mirrors masking.Fake exactly on
// PostgreSQL 11+, where sha256() exists.
func maskExpr(c masking.PlanColumn, h string, args *[]any) string {
	col := pgx.Identifier{c.Column.Name}.Sanitize()
	n1 := "(('x' || substr(" + h + ", 1, 7))::bit(28)::int)"
	n2 := "(('x' || substr(" + h + ", 8, 7))::bit(28)::int)"
	pick := func(list []string, n string) string {
		quoted := make([]string, len(list))
		for i, v := range list {
			quoted[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		return "(ARRAY[" + strings.Join(quoted, ",") + "])[(" + n + " % " + strconv.Itoa(len(list)) + ") + 1]"
	}
	var expr string
	switch c.Rule {
	case masking.RuleEmail:
		expr = "'user_' || substr(" + h + ", 1, 10) || '@example.com'"
	case masking.RuleFirstName:
		expr = pick(masking.FirstNames, n1)
	case masking.RuleLastName:
		expr = pick(masking.LastNames, n2)
	case masking.RuleName:
		expr = pick(masking.FirstNames, n1) + " || ' ' || " + pick(masking.LastNames, n2)
	case masking.RulePhone:
		expr = "'+1 555 ' || lpad((" + n1 + " % 1000)::text, 3, '0') || ' ' || lpad((" + n2 + " % 10000)::text, 4, '0')"
	case masking.RuleHash:
		expr = h
		if c.Column.MaxLength > 0 {
			expr = "left(" + h + ", " + strconv.Itoa(c.Column.MaxLength) + ")"
		}
	case masking.RuleDateShift:
		expr = "(" + col + " + ((" + n1 + " % 361) - 180) * interval '1 day')::" + c.Column.Type
	case masking.RuleRedact:
		*args = append(*args, c.Value)
		return col + " = CASE WHEN " + col + " IS NULL THEN NULL ELSE $" + strconv.Itoa(len(*args)) + " END"
	case masking.RuleNull:
		return col + " = NULL"
	}
	return col + " = CASE WHEN " + col + " IS NULL THEN NULL ELSE " + expr + " END"
}

// Mask applies a plan inside one transaction with triggers and foreign-key
// enforcement off (session_replication_role = replica, which needs the
// sandbox's superuser), then checks the result before committing.
func (d *Driver) Mask(ctx context.Context, t engine.Target, dbName string, plan masking.Plan, log engine.Logger) (masking.Report, error) {
	start := time.Now()
	var rep masking.Report
	conn, done, err := d.connect(ctx, t, dbName)
	if err != nil {
		return rep, err
	}
	defer done()
	version, err := serverVersion(ctx, conn)
	if err != nil {
		return rep, err
	}
	digest := "encode(sha256(convert_to($1 || COL::text, 'UTF8')), 'hex')"
	if version < 110000 {
		// No sha256() before PostgreSQL 11. The key still makes it keyed;
		// fakes differ from other engines' but stay deterministic.
		digest = "md5($1 || COL::text)"
		log.Warnf("PostgreSQL %d has no sha256(); masking uses a keyed md5 instead", version/10000)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		return rep, fmt.Errorf("masking needs a superuser connection to the sandbox (to turn triggers off): %w", err)
	}

	var truncate []string
	for _, pt := range plan.Tables {
		name := pgx.Identifier{pt.Table.Schema, pt.Table.Name}.Sanitize()
		label := pt.Table.Table().String()
		if pt.Truncate {
			var n int64
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+name).Scan(&n); err != nil {
				return rep, err
			}
			truncate = append(truncate, name)
			rep.Tables = append(rep.Tables, masking.TableReport{Table: label, Action: "truncated", Rows: n})
			continue
		}
		// $1 is the key, bound only when a rule derives values from it.
		var args []any
		for _, c := range pt.Columns {
			if c.Rule != masking.RuleRedact && c.Rule != masking.RuleNull {
				args = []any{plan.Key}
				break
			}
		}
		sets := make([]string, 0, len(pt.Columns))
		cols := make([]string, 0, len(pt.Columns))
		for _, c := range pt.Columns {
			h := strings.ReplaceAll(digest, "COL", pgx.Identifier{c.Column.Name}.Sanitize())
			sets = append(sets, maskExpr(c, h, &args))
			cols = append(cols, c.Column.Name+": "+c.Rule)
		}
		stmt := "UPDATE " + name + " SET " + strings.Join(sets, ", ")
		tag, err := tx.Exec(ctx, stmt, args...)
		if err != nil {
			return rep, fmt.Errorf("masking %s: %w", label, err)
		}
		rep.Tables = append(rep.Tables, masking.TableReport{Table: label, Action: "masked", Rows: tag.RowsAffected(), Columns: cols})
		rep.RowsChanged += tag.RowsAffected()
		log.Infof("Masked %s: %d rows (%s)", label, tag.RowsAffected(), strings.Join(cols, ", "))
	}
	if len(truncate) > 0 {
		// One statement, so tables that reference each other can be emptied together.
		if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(truncate, ", ")); err != nil {
			return rep, fmt.Errorf("truncating: %w", err)
		}
		for _, tr := range rep.Tables {
			if tr.Action == "truncated" {
				rep.RowsChanged += tr.Rows
				log.Infof("Truncated %s (%d rows)", tr.Table, tr.Rows)
			}
		}
	}

	for _, c := range masking.Checks(plan) {
		name := pgx.Identifier{c.Table.Schema, c.Table.Name}.Sanitize()
		q := masking.CheckSQL(c, name, pgx.Identifier{c.Column}.Sanitize(), "$1")
		var args []any
		if c.Kind == "equals" {
			args = []any{c.Value}
		}
		var bad int64
		if err := tx.QueryRow(ctx, q, args...).Scan(&bad); err != nil {
			return rep, fmt.Errorf("checking that %s: %w", c.Description, err)
		}
		if bad > 0 {
			return rep, fmt.Errorf("masking check failed: expected %s, but %d rows don't match", c.Description, bad)
		}
		rep.Checks++
	}
	if err := tx.Commit(ctx); err != nil {
		return rep, err
	}
	rep.DurationMs = time.Since(start).Milliseconds()
	return rep, nil
}
