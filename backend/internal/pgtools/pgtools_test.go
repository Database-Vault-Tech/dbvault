package pgtools

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const sampleTOC = `;
; Archive created at 2026-09-19 10:47:09 UTC
;     dbname: shop
;
216; 1259 16390 TABLE public customers app
218; 1259 16402 TABLE public orders app
220; 1259 16420 TABLE analytics events app
3380; 0 16390 TABLE DATA public customers app
3381; 0 16402 TABLE DATA public orders app
217; 1259 16389 SEQUENCE public customers_id_seq app
3390; 0 0 SEQUENCE SET public customers_id_seq app
222; 1259 16430 VIEW public big_spenders app
3200; 2606 16397 CONSTRAINT public customers customers_pkey app
`

func TestParseTOC(t *testing.T) {
	entries := ParseTOC(strings.NewReader(sampleTOC))
	if got := CountTables(entries); got != 3 {
		t.Fatalf("CountTables = %d, want 3", got)
	}
	var names []string
	for _, e := range entries {
		if e.Type == "TABLE" {
			names = append(names, e.Schema+"."+e.Name)
		}
	}
	want := []string{"public.customers", "public.orders", "analytics.events"}
	if !slices.Equal(names, want) {
		t.Fatalf("tables %v, want %v", names, want)
	}
	types := map[string]int{}
	for _, e := range entries {
		types[e.Type]++
	}
	if types["TABLE DATA"] != 2 || types["SEQUENCE SET"] != 1 || types["VIEW"] != 1 || types["CONSTRAINT"] != 1 {
		t.Fatalf("unexpected type counts %v", types)
	}
}

func TestRestoreArgs(t *testing.T) {
	args := RestoreArgs(RestoreOptions{Clean: true, SingleTransaction: true})
	for _, want := range []string{"--no-owner", "--no-privileges", "--exit-on-error", "--clean", "--if-exists", "--single-transaction"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	if args[len(args)-1] != "--dbname=" {
		t.Error("database name must come from PGDATABASE, not argv")
	}
	plain := RestoreArgs(RestoreOptions{})
	if slices.Contains(plain, "--clean") {
		t.Error("clean must be opt-in")
	}
}

func TestDumpArgsNeverContainCredentials(t *testing.T) {
	for _, a := range DumpArgs() {
		if strings.Contains(strings.ToLower(a), "password=") || strings.Contains(a, "@") {
			t.Fatalf("credentials must never be passed in argv: %v", DumpArgs())
		}
	}
}

func TestTailBufferSummary(t *testing.T) {
	tb := &tailBuffer{max: 64}
	_, _ = tb.Write([]byte(strings.Repeat("x", 100)))
	if len(tb.String()) != 64 {
		t.Fatalf("tail buffer kept %d bytes", len(tb.String()))
	}
	tb2 := &tailBuffer{max: 4096}
	_, _ = tb2.Write([]byte("pg_dump: connecting\npg_dump: error: query failed: permission denied for table secrets\n"))
	s := tb2.Summary(errors.New("exit status 1"))
	if !strings.Contains(s, "permission denied") || strings.Contains(s, "connecting") {
		t.Fatalf("summary should keep the error line only: %q", s)
	}
	if (&tailBuffer{max: 10}).Summary(errors.New("exit status 2")) != "exit status 2" {
		t.Fatal("empty stderr should fall back to the exit error")
	}
}

func TestAdaptLineDropsOnlyUnknownHeaderSettings(t *testing.T) {
	pg15 := map[string]bool{"statement_timeout": true, "lock_timeout": true, "client_encoding": true}
	if got := adaptLine("SET transaction_timeout = 0;\n", 12, pg15); !strings.HasPrefix(got, "-- DBVault: skipped") {
		t.Fatalf("unknown setting must be commented out, got %q", got)
	}
	if got := adaptLine("SET statement_timeout = 0;\n", 10, pg15); got != "SET statement_timeout = 0;\n" {
		t.Fatalf("known setting must pass through, got %q", got)
	}
	// Past the header (e.g. inside COPY data) nothing is ever rewritten.
	if got := adaptLine("SET transaction_timeout = 0;\n", headerLines+5, pg15); got != "SET transaction_timeout = 0;\n" {
		t.Fatalf("data lines must never change, got %q", got)
	}
	if got := adaptLine("CREATE TABLE t (id int);\n", 1, pg15); got != "CREATE TABLE t (id int);\n" {
		t.Fatalf("non-SET lines must pass through, got %q", got)
	}
}
