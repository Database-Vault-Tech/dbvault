package integration

import (
	"context"
	"testing"
	"time"

	"github.com/dbvault/dbvault/backend/internal/masking"
)

// TestPostgresMasking masks a database the way a masked restore does inside
// its sandbox, and checks nothing real survives, triggers stay silent, and
// fakes are consistent across tables and identical to the Go generator.
func TestPostgresMasking(t *testing.T) {
	admin := adminTarget(t)
	ctx := context.Background()
	db := createDB(t, admin)
	conn := connect(t, admin, db)
	if _, err := conn.Exec(ctx, `
		CREATE TABLE users (
			id serial PRIMARY KEY,
			email varchar(120) NOT NULL UNIQUE,
			full_name text,
			phone text,
			date_of_birth date,
			password_hash text NOT NULL,
			plan text NOT NULL DEFAULT 'free');
		CREATE TABLE orders (
			id bigserial PRIMARY KEY,
			user_id int NOT NULL REFERENCES users(id),
			customer_email text,
			shipping_address text,
			total numeric(10,2));
		CREATE TABLE sessions (id text PRIMARY KEY, user_id int REFERENCES users(id), ip_address text);
		CREATE TABLE changes (id serial PRIMARY KEY, old_value text);
		-- An audit trigger that would copy real emails elsewhere if it fired.
		CREATE FUNCTION audit_users() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN INSERT INTO changes (old_value) VALUES (OLD.email); RETURN NEW; END $$;
		CREATE TRIGGER users_audit BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION audit_users();
		INSERT INTO users (email, full_name, phone, date_of_birth, password_hash) VALUES
			('ada@example.org', 'Ada Lovelace', '+44 20 7946 0958', '1990-12-10', 'argon2-secret'),
			('grace@navy.mil', 'Grace Hopper', NULL, '1985-06-01', 'argon2-secret-2'),
			('alan@bletchley.uk', NULL, '+44 1908 640404', NULL, 'argon2-secret-3');
		INSERT INTO orders (user_id, customer_email, shipping_address, total)
			SELECT 1 + (g % 3), (ARRAY['ada@example.org','grace@navy.mil','alan@bletchley.uk'])[1 + (g % 3)], g || ' Real Street', g
			FROM generate_series(1, 30) g;
		INSERT INTO sessions VALUES ('s1', 1, '203.0.113.9'), ('s2', 2, '198.51.100.7');
	`); err != nil {
		t.Fatal(err)
	}

	drv := pgDriver(t)
	cat, err := drv.Catalog(ctx, admin, db)
	if err != nil {
		t.Fatal(err)
	}
	rules := masking.Suggest(cat)
	// The copy of the email in orders isn't named like an email column, so
	// write that rule by hand; suggestions don't cover everything.
	rules.Tables["orders"].Columns["customer_email"] = masking.ColumnRule{Rule: masking.RuleEmail}
	if !rules.Tables["sessions"].Truncate || rules.Tables["users"].Columns["email"].Rule != masking.RuleEmail {
		t.Fatalf("unexpected suggestions: %+v", rules.Tables)
	}
	key, _ := masking.NewKey()
	plan, err := masking.Build(rules, cat, key)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	rep, err := drv.Mask(ctx, admin, db, plan, logSink{t})
	if err != nil {
		t.Fatalf("mask: %v", err)
	}
	if rep.Checks == 0 || rep.RowsChanged < 3+30+2 {
		t.Fatalf("report %+v", rep)
	}

	var leaked int
	if err := conn.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users WHERE email NOT LIKE 'user\_%@example.com' OR password_hash LIKE 'argon2%' OR full_name IN ('Ada Lovelace','Grace Hopper') OR phone LIKE '+44%')
		+ (SELECT count(*) FROM orders WHERE customer_email NOT LIKE 'user\_%@example.com' OR shipping_address LIKE '%Real Street')
		+ (SELECT count(*) FROM sessions)
		+ (SELECT count(*) FROM changes)`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d rows still hold real data (or the audit trigger fired)", leaked)
	}

	// Same original → same fake, in both tables and in Go.
	want, _ := masking.Fake(masking.RuleEmail, key, "ada@example.org", 0)
	var got string
	if err := conn.QueryRow(ctx, `SELECT email FROM users WHERE id = 1`).Scan(&got); err != nil || got != want {
		t.Fatalf("users.email = %q (%v), Go generator says %q", got, err, want)
	}
	var mismatched int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM orders o JOIN users u ON u.id = o.user_id WHERE o.customer_email <> u.email`).Scan(&mismatched); err != nil || mismatched != 0 {
		t.Fatalf("%d orders have a fake email that differs from their user's (%v)", mismatched, err)
	}
	wantName, _ := masking.Fake(masking.RuleName, key, "Ada Lovelace", 0)
	wantPhone, _ := masking.Fake(masking.RulePhone, key, "+44 20 7946 0958", 0)
	var name, phone string
	var dob time.Time
	if err := conn.QueryRow(ctx, `SELECT full_name, phone, date_of_birth FROM users WHERE id = 1`).Scan(&name, &phone, &dob); err != nil {
		t.Fatal(err)
	}
	if name != wantName || phone != wantPhone {
		t.Fatalf("SQL and Go fakes differ: %q/%q vs %q/%q", name, phone, wantName, wantPhone)
	}
	wantDob, _ := masking.ShiftDateText(key, "1990-12-10")
	if dob.Format("2006-01-02") != wantDob {
		t.Fatalf("date_of_birth %s, want %s", dob.Format("2006-01-02"), wantDob)
	}
	var nulls int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM users WHERE (id = 3 AND full_name IS NOT NULL) OR (id = 2 AND phone IS NOT NULL)`).Scan(&nulls); err != nil || nulls != 0 {
		t.Fatalf("NULLs must stay NULL: %d %v", nulls, err)
	}
	// The trigger is still there afterwards, so the masked copy behaves like production.
	var triggers int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgname = 'users_audit'`).Scan(&triggers); err != nil || triggers != 1 {
		t.Fatalf("trigger lost: %d %v", triggers, err)
	}

	// Any failure rolls the whole run back: nothing is left half-masked.
	// Two rules on one column make the UPDATE fail after other tables
	// were already masked in the same transaction.
	var users, orders masking.PlanTable
	for _, pt := range plan.Tables {
		switch pt.Table.Name {
		case "users":
			users = pt
		case "orders":
			orders = pt
		}
	}
	users.Columns = append(append([]masking.PlanColumn(nil), users.Columns...), users.Columns[0])
	var before string
	if err := conn.QueryRow(ctx, `SELECT customer_email FROM orders WHERE id = 1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	failing := masking.Plan{Key: "another-key", Tables: []masking.PlanTable{orders, users}}
	if _, err := drv.Mask(ctx, admin, db, failing, logSink{t}); err == nil {
		t.Fatal("a failing masking statement must return an error")
	}
	var after string
	if err := conn.QueryRow(ctx, `SELECT customer_email FROM orders WHERE id = 1`).Scan(&after); err != nil || after != before {
		t.Fatalf("orders was changed by a failed run: %q → %q (%v)", before, after, err)
	}
}
