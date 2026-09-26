package masking

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

func shop() engine.Catalog {
	text := func(name string) engine.CatalogColumn {
		return engine.CatalogColumn{Name: name, Type: "text", Nullable: true}
	}
	return engine.Catalog{Tables: []engine.CatalogTable{
		{Schema: "public", Name: "users", Columns: []engine.CatalogColumn{
			{Name: "id", Type: "integer", Key: true},
			{Name: "email", Type: "character varying(255)", Unique: true, MaxLength: 255},
			text("full_name"),
			text("phone"),
			{Name: "date_of_birth", Type: "date", Nullable: true},
			{Name: "password_hash", Type: "text"},
			{Name: "plan", Type: "text"},
			{Name: "age", Type: "integer", Nullable: true},
		}},
		{Schema: "public", Name: "orders", References: []string{"public.users"}, Columns: []engine.CatalogColumn{
			{Name: "id", Type: "bigint", Key: true},
			{Name: "user_id", Type: "integer", Key: true},
			text("shipping_address"),
			{Name: "total", Type: "numeric"},
		}},
		{Schema: "public", Name: "sessions", References: []string{"public.users"}, Columns: []engine.CatalogColumn{
			{Name: "id", Type: "text", Key: true}, {Name: "user_id", Type: "integer", Key: true}, text("ip_address"),
		}},
		{Schema: "billing", Name: "payments", References: []string{"public.orders"}, Columns: []engine.CatalogColumn{
			{Name: "id", Type: "bigint", Key: true}, {Name: "order_id", Type: "bigint", Key: true}, text("card_last4"),
		}},
	}}
}

func TestRulesJSONRoundTrip(t *testing.T) {
	in := `{"tables":{"users":{"email":"email","password_hash":{"redact":"x"}},"sessions":"truncate"}}`
	var r Rules
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Tables["sessions"].Truncate || r.Tables["users"].Columns["password_hash"] != (ColumnRule{Rule: RuleRedact, Value: "x"}) {
		t.Fatalf("parsed %+v", r)
	}
	out, _ := json.Marshal(r)
	var again Rules
	if err := json.Unmarshal(out, &again); err != nil || len(again.Tables) != 2 {
		t.Fatalf("round trip: %s %v", out, err)
	}
	for _, bad := range []string{`{"tables":{"t":"drop"}}`, `{"tables":{"t":{"c":{"hash":"x"}}}}`, `{"tables":{"t":5}}`} {
		if json.Unmarshal([]byte(bad), &r) == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
	if p := (Rules{Tables: map[string]TableRule{"t": {Columns: map[string]ColumnRule{"c": {Rule: "shuffle"}}}}}).Validate(); len(p) != 1 {
		t.Errorf("unknown rule not reported: %v", p)
	}
}

func TestSuggest(t *testing.T) {
	r := Suggest(shop())
	users := r.Tables["users"].Columns
	want := map[string]string{"email": RuleEmail, "full_name": RuleName, "phone": RulePhone, "date_of_birth": RuleDateShift, "password_hash": RuleRedact}
	for c, rule := range want {
		if users[c].Rule != rule {
			t.Errorf("users.%s suggested %q, want %q", c, users[c].Rule, rule)
		}
	}
	if _, ok := users["plan"]; ok {
		t.Error("non-personal column suggested")
	}
	if _, ok := users["id"]; ok {
		t.Error("key column suggested")
	}
	if r.Tables["orders"].Columns["shipping_address"].Rule != RuleRedact {
		t.Errorf("orders: %+v", r.Tables["orders"])
	}
	if !r.Tables["sessions"].Truncate {
		t.Error("sessions should be truncated")
	}
	if r.Tables["billing.payments"].Columns["card_last4"].Rule != RuleRedact {
		t.Errorf("non-default schema: %+v", r.Tables)
	}
	// The suggestions pass their own validation.
	if _, err := Build(r, shop(), "k"); err != nil {
		t.Fatalf("suggested rules don't build: %v", err)
	}
}

func problems(t *testing.T, err error) string {
	t.Helper()
	var p Problems
	if !errors.As(err, &p) {
		t.Fatalf("expected problems, got %v", err)
	}
	return strings.Join(p, "\n")
}

func TestBuildFailsClosed(t *testing.T) {
	r := Suggest(shop())
	// Drift: a new personal column appears after the rules were written.
	cat := shop()
	cat.Tables[0].Columns = append(cat.Tables[0].Columns, engine.CatalogColumn{Name: "recovery_email", Type: "text", Nullable: true})
	_, err := Build(r, cat, "k")
	if got := problems(t, err); !strings.Contains(got, "users.recovery_email looks like personal data") {
		t.Fatalf("drift not reported: %s", got)
	}
	// Marking it keep is an explicit decision and passes.
	r.Tables["users"].Columns["recovery_email"] = ColumnRule{Rule: RuleKeep}
	if _, err := Build(r, cat, "k"); err != nil {
		t.Fatalf("keep should satisfy drift: %v", err)
	}
}

func TestBuildRejectsUnsafeRules(t *testing.T) {
	cases := map[string]struct {
		rules Rules
		want  string
	}{
		"key column":       {Rules{Tables: map[string]TableRule{"orders": {Columns: map[string]ColumnRule{"user_id": {Rule: RuleHash}}}}}, "key columns can't be masked"},
		"unique name":      {Rules{Tables: map[string]TableRule{"users": {Columns: map[string]ColumnRule{"email": {Rule: RuleName}}}}}, "can produce duplicates"},
		"text on number":   {Rules{Tables: map[string]TableRule{"users": {Columns: map[string]ColumnRule{"age": {Rule: RuleEmail}}}}}, "writes text"},
		"null not null":    {Rules{Tables: map[string]TableRule{"users": {Columns: map[string]ColumnRule{"password_hash": {Rule: RuleNull}}}}}, "NOT NULL"},
		"date on text":     {Rules{Tables: map[string]TableRule{"users": {Columns: map[string]ColumnRule{"plan": {Rule: RuleDateShift}}}}}, "date or timestamp"},
		"missing table":    {Rules{Tables: map[string]TableRule{"ghosts": {Truncate: true}}}, "ghosts doesn't exist"},
		"missing column":   {Rules{Tables: map[string]TableRule{"users": {Columns: map[string]ColumnRule{"nope": {Rule: RuleHash}}}}}, "users.nope doesn't exist"},
		"parent truncated": {Rules{Tables: map[string]TableRule{"users": {Truncate: true}}}, "users can't be truncated because orders references it"},
		"redact too long":  {Rules{Tables: map[string]TableRule{"users": {Columns: map[string]ColumnRule{"email": {Rule: RuleRedact, Value: strings.Repeat("x", 300)}}}}}, "can produce duplicates"},
	}
	for name, c := range cases {
		_, err := Build(c.rules, shop(), "k")
		if got := problems(t, err); !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q don't mention %q", name, got, c.want)
		}
	}
	// Truncating a parent together with every table that references it is fine.
	all := Rules{Tables: map[string]TableRule{"users": {Truncate: true}, "orders": {Truncate: true}, "sessions": {Truncate: true}, "billing.payments": {Truncate: true}}}
	if _, err := Build(all, shop(), "k"); err != nil {
		t.Errorf("truncating a whole reference chain: %v", err)
	}
}

func TestFakesAreDeterministicAndKeyed(t *testing.T) {
	a, _ := Fake(RuleEmail, "key-1", "ada@example.org", 0)
	b, _ := Fake(RuleEmail, "key-1", "ada@example.org", 0)
	c, _ := Fake(RuleEmail, "key-2", "ada@example.org", 0)
	if a != b || a == c || !strings.HasPrefix(a, "user_") || !strings.HasSuffix(a, "@example.com") || len(a) != len("user_")+10+len("@example.com") {
		t.Fatalf("email fakes: %q %q %q", a, b, c)
	}
	name, _ := Fake(RuleName, "k", "Ada Lovelace", 0)
	if parts := strings.SplitN(name, " ", 2); len(parts) != 2 {
		t.Errorf("name %q", name)
	}
	phone, _ := Fake(RulePhone, "k", "+44 20 7946 0958", 0)
	if len(phone) != len("+1 555 000 0000") || !strings.HasPrefix(phone, "+1 555 ") {
		t.Errorf("phone %q", phone)
	}
	if h, _ := Fake(RuleHash, "k", "x", 8); len(h) != 8 {
		t.Errorf("hash not shortened: %q", h)
	}
	d, err := ShiftDateText("k", "1990-12-10")
	if err != nil || len(d) != 10 || d == "1990-12-10" && ShiftDays("k", "1990-12-10") != 0 {
		t.Errorf("date shift %q %v", d, err)
	}
	if days := ShiftDays("k", "anything"); days < -180 || days > 180 {
		t.Errorf("shift %d out of range", days)
	}
	if _, err := ShiftDateText("k", "Dec 10th, 1990"); err == nil || strings.Contains(err.Error(), "Dec") {
		t.Errorf("unparseable dates must fail without echoing the value: %v", err)
	}
	if ex := Examples("k"); !strings.Contains(ex[RuleEmail], "→ user_") {
		t.Errorf("examples: %v", ex)
	}
}
