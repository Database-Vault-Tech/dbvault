package mysql

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

func TestParseVersion(t *testing.T) {
	for _, c := range []struct {
		in         string
		major, num int
		short      string
	}{
		{"8.4.3", 8, 80403, "8.4.3"},
		{"11.4.12-MariaDB-ubu2404", 11, 110412, "11.4.12"},
		{"5.7.44-log", 5, 50744, "5.7.44"},
	} {
		major, num, short := parseVersion(c.in)
		if major != c.major || num != c.num || short != c.short {
			t.Errorf("parseVersion(%q) = %d %d %q", c.in, major, num, short)
		}
	}
	if !isMariaDB("10.6.1-MariaDB", "") || isMariaDB("8.4.3", "MySQL Community Server - GPL") {
		t.Error("flavor detection")
	}
}

func TestOptionFileQuotesCredentials(t *testing.T) {
	got := optionFileContent(engine.Target{Host: "db", Port: 3306, Username: "app", Password: "p\"w\\x\nnext=1", SSLMode: "disable"}, "")
	if !strings.Contains(got, `password="p\"w\\x\nnext=1"`) {
		t.Fatalf("password not escaped:\n%s", got)
	}
	if strings.Count(got, "\n") != 7 || !strings.Contains(got, "protocol=TCP\n") || !strings.Contains(got, "skip-ssl\n") {
		t.Fatalf("unexpected option file:\n%s", got)
	}
	full := optionFileContent(engine.Target{SSLMode: "verify-full"}, "/w/ca.pem")
	if !strings.Contains(full, "ssl-verify-server-cert\n") || !strings.Contains(full, `ssl-ca="/w/ca.pem"`) {
		t.Fatalf("verify-full:\n%s", full)
	}
	if prefer := optionFileContent(engine.Target{SSLMode: "prefer"}, ""); !strings.Contains(prefer, "skip-ssl-verify-server-cert") {
		t.Fatalf("prefer:\n%s", prefer)
	}
}

func TestDumpArgsNeverContainCredentials(t *testing.T) {
	for _, a := range DumpArgs() {
		if strings.Contains(strings.ToLower(a), "password") || strings.Contains(a, "--user") {
			t.Fatalf("credentials must never be passed in argv: %v", DumpArgs())
		}
	}
}

const dump = sandboxLine + " \n" + "-- MariaDB dump\n" +
	"/*!50001 CREATE VIEW `big_orders` AS SELECT\n" +
	"CREATE TABLE `customers` (\n" +
	"INSERT INTO `customers` VALUES (1,'DEFINER=`x`@`y`');\n" +
	"CREATE TABLE `we``ird` (\n" +
	"/*!50003 CREATE*/ /*!50017 DEFINER=`root`@`%`*/ /*!50003 TRIGGER `t` BEFORE INSERT ON `orders` FOR EACH ROW SET NEW.a = 1 */;;\n" +
	"/*!50013 DEFINER=`app``1`@`10.%` SQL SECURITY DEFINER */\n"

func TestStripSandbox(t *testing.T) {
	b, _ := io.ReadAll(stripSandbox(strings.NewReader(dump)))
	if strings.Contains(string(b), "sandbox") || !strings.HasPrefix(string(b), "-- MariaDB dump") {
		t.Fatalf("sandbox line not stripped: %q", b[:40])
	}
	plain := "-- MySQL dump\nCREATE TABLE `a` (\n"
	if b, _ := io.ReadAll(stripSandbox(strings.NewReader(plain))); string(b) != plain {
		t.Fatalf("dump without sandbox line changed: %q", b)
	}
}

func TestRestoreFilter(t *testing.T) {
	f := newRestoreFilter(strings.NewReader(dump))
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if !strings.HasPrefix(out, sandboxLine+"\n-- MariaDB dump") || strings.Count(out, "sandbox") != 1 {
		t.Fatalf("sandbox line must come first exactly once:\n%s", out)
	}
	if f.rewritten != 2 || strings.Contains(out, "`root`@") || strings.Contains(out, "`app``1`@") {
		t.Fatalf("definers not rewritten (%d):\n%s", f.rewritten, out)
	}
	if !strings.Contains(out, "'DEFINER=`x`@`y`'") {
		t.Fatalf("data must not be rewritten:\n%s", out)
	}
}

func TestRestoreFilterLongLines(t *testing.T) {
	long := "INSERT INTO `t` VALUES " + strings.Repeat("('DEFINER=`a`@`b`'),", 100000) + "(1);\n"
	in := long + "/*!50013 DEFINER=`root`@`%` SQL SECURITY DEFINER */\n"
	f := newRestoreFilter(strings.NewReader(in))
	b, _ := io.ReadAll(f)
	if len(b) != len(sandboxLine)+1+len(in)-len("`root`@`%`")+len("CURRENT_USER") || f.rewritten != 1 {
		t.Fatalf("long line handling: len %d rewritten %d", len(b), f.rewritten)
	}
}

func TestListTables(t *testing.T) {
	tables, err := listTables(context.Background(), strings.NewReader(dump))
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 || tables[0].Name != "customers" || tables[1].Name != "we`ird" {
		t.Fatalf("tables = %+v", tables)
	}
}

func TestSandboxImages(t *testing.T) {
	my, maria := NewMySQL("", ""), NewMariaDB("", "")
	for _, c := range []struct {
		d     *Driver
		major int
		image string
	}{{my, 0, "mysql:8.4"}, {my, 8, "mysql:8"}, {my, 5, "mysql:5.7"}, {my, 9, "mysql:9"}, {maria, 10, "mariadb:10"}, {maria, 0, "mariadb:11"}} {
		if got := c.d.Sandbox(c.major, "pw").Image; got != c.image {
			t.Errorf("%s %d: image %s, want %s", c.d.Name(), c.major, got, c.image)
		}
	}
}
