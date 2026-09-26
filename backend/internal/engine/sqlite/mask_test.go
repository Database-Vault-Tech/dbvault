package sqlite

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/masking"
)

func TestMaskSQLite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	d := New(root, t.TempDir())
	db, err := open(filepath.Join(root, "app.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email VARCHAR(120) NOT NULL UNIQUE, full_name TEXT, phone TEXT,
			date_of_birth DATE, joined DATETIME, password_hash TEXT NOT NULL, plan TEXT NOT NULL DEFAULT 'free')`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id), customer_email TEXT, shipping_address TEXT)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id INTEGER REFERENCES users(id), ip_address TEXT)`,
		`CREATE TABLE changes (id INTEGER PRIMARY KEY, old_value TEXT)`,
		// Would copy real emails into changes if it fired during masking.
		`CREATE TRIGGER users_audit AFTER UPDATE ON users BEGIN INSERT INTO changes (old_value) VALUES (OLD.email); END`,
		`INSERT INTO users (email, full_name, phone, date_of_birth, joined, password_hash) VALUES
			('ada@example.org', 'Ada Lovelace', '+44 20 7946 0958', '1990-12-10', 1700000000, 'argon2-secret'),
			('grace@navy.mil', 'Grace Hopper', NULL, '1985-06-01 08:30:00', NULL, 'argon2-secret-2')`,
		`INSERT INTO orders (user_id, customer_email, shipping_address) VALUES (1, 'ada@example.org', '1 Real Street'), (2, 'grace@navy.mil', '2 Real Street')`,
		`INSERT INTO sessions VALUES ('s1', 1, '203.0.113.9')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	cat, err := d.Catalog(ctx, engine.Target{}, "app.db")
	if err != nil {
		t.Fatal(err)
	}
	var users engine.CatalogTable
	for _, tb := range cat.Tables {
		if tb.Name == "users" {
			users = tb
		}
		if tb.Name == "orders" && (len(tb.References) != 1 || tb.References[0] != "users") {
			t.Fatalf("orders references %v", tb.References)
		}
	}
	for _, c := range users.Columns {
		switch c.Name {
		case "id":
			if !c.Key || !c.Unique {
				t.Errorf("id: %+v", c)
			}
		case "email":
			if !c.Unique || c.Nullable || c.MaxLength != 120 {
				t.Errorf("email: %+v", c)
			}
		}
	}

	rules := masking.Suggest(cat)
	rules.Tables["orders"].Columns["customer_email"] = masking.ColumnRule{Rule: masking.RuleEmail}
	rules.Tables["users"].Columns["joined"] = masking.ColumnRule{Rule: masking.RuleDateShift}
	key, _ := masking.NewKey()
	plan, err := masking.Build(rules, cat, key)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := d.Mask(ctx, engine.Target{}, "app.db", plan, logSink{t})
	if err != nil {
		t.Fatalf("mask: %v", err)
	}
	if rep.Checks == 0 {
		t.Fatalf("report %+v", rep)
	}

	db, _ = open(filepath.Join(root, "app.db"), true)
	defer db.Close()
	var email, name, dob, orderEmail string
	var joined int64
	// "|| ''" reads the stored text: the driver would otherwise parse DATE
	// columns into time values. The format must be unchanged (YYYY-MM-DD).
	if err := db.QueryRow(`SELECT u.email, u.full_name, u.date_of_birth || '', u.joined, o.customer_email FROM users u JOIN orders o ON o.user_id = u.id WHERE u.id = 1`).
		Scan(&email, &name, &dob, &joined, &orderEmail); err != nil {
		t.Fatal(err)
	}
	wantEmail, _ := masking.Fake(masking.RuleEmail, key, "ada@example.org", 0)
	wantName, _ := masking.Fake(masking.RuleName, key, "Ada Lovelace", 0)
	wantDob, _ := masking.ShiftDateText(key, "1990-12-10")
	if email != wantEmail || orderEmail != wantEmail || name != wantName || dob != wantDob {
		t.Fatalf("got %q %q %q %q; want %q %q %q", email, orderEmail, name, dob, wantEmail, wantName, wantDob)
	}
	if shift := masking.ShiftDays(key, "1700000000"); joined != 1700000000+int64(shift)*86400 {
		t.Fatalf("unix-seconds date shifted to %d", joined)
	}
	var fired, sessions int
	_ = db.QueryRow(`SELECT (SELECT count(*) FROM changes), (SELECT count(*) FROM sessions)`).Scan(&fired, &sessions)
	var triggers int
	_ = db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'trigger' AND name = 'users_audit'`).Scan(&triggers)
	if fired != 0 || sessions != 0 || triggers != 1 {
		t.Fatalf("trigger fired %d times, %d sessions left, %d triggers after masking", fired, sessions, triggers)
	}

	// The copy that leaves the sandbox (a VACUUM INTO snapshot) holds none of
	// the original values, even though the masked file's free pages may.
	snap := dump(t, d, "app.db")
	for _, real := range []string{"ada@example.org", "grace@navy.mil", "Lovelace", "argon2-secret", "Real Street", "203.0.113.9"} {
		if bytes.Contains(snap, []byte(real)) {
			t.Errorf("snapshot still contains %q", real)
		}
	}

	// Values date_shift can't parse fail the run (and it rolls back) rather
	// than keeping a real birth date.
	db2, _ := open(filepath.Join(root, "app.db"), false)
	if _, err := db2.Exec(`UPDATE users SET date_of_birth = 'tenth of December' WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	db2.Close()
	before, _ := os.ReadFile(filepath.Join(root, "app.db"))
	_, err = d.Mask(ctx, engine.Target{}, "app.db", plan, logSink{t})
	if err == nil || !strings.Contains(err.Error(), "isn't a date") || strings.Contains(err.Error(), "December") {
		t.Fatalf("unparseable date: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "app.db"))
	if !bytes.Equal(before, after) {
		t.Fatal("a failed masking run changed the file")
	}
}
