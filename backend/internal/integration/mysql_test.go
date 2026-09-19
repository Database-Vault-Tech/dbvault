package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/dbvault/dbvault/backend/internal/backups"
	"github.com/dbvault/dbvault/backend/internal/encryption"
	"github.com/dbvault/dbvault/backend/internal/engine"
	"github.com/dbvault/dbvault/backend/internal/engine/mysql"
	"github.com/dbvault/dbvault/backend/internal/storage"
)

// mysqlFlavors are exercised when their server URL is set, e.g.
// DBVAULT_TEST_MYSQL_URL=mysql://root:root@127.0.0.1:3306/.
var mysqlFlavors = []struct {
	name, env string
	driver    func(workDir string) *mysql.Driver
}{
	{engine.MySQL, "DBVAULT_TEST_MYSQL_URL", func(w string) *mysql.Driver { return mysql.NewMySQL("", w) }},
	{engine.MariaDB, "DBVAULT_TEST_MARIADB_URL", func(w string) *mysql.Driver { return mysql.NewMariaDB("", w) }},
}

func mysqlAdmin(t *testing.T, engineName, env string) engine.Target {
	t.Helper()
	raw := os.Getenv(env)
	if raw == "" {
		t.Skip(env + " not set")
	}
	if _, err := exec.LookPath("mariadb-dump"); err != nil {
		t.Skip("mariadb-dump not installed")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	pw, _ := u.User.Password()
	return engine.Target{Engine: engineName, Host: u.Hostname(), Port: port, Username: u.User.Username(), Password: pw, SSLMode: "prefer"}
}

func mysqlConn(t *testing.T, admin engine.Target, db string) *sql.DB {
	t.Helper()
	cfg := gomysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = admin.Username, admin.Password, "tcp", fmt.Sprintf("%s:%d", admin.Host, admin.Port), db
	// Plain TCP: MySQL 5.7's legacy TLS ciphers are refused by Go.
	cfg.TLSConfig = "false"
	c, err := gomysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn := sql.OpenDB(c)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func mysqlExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// TestMySQLBackupAndRestoreRoundTrip mirrors the PostgreSQL round trip for
// MySQL and MariaDB: dump → zstd → age → sha256 → storage, verified
// download, restore into a fresh database as a different user than the
// objects' definer, and compare data, the view and the trigger.
func TestMySQLBackupAndRestoreRoundTrip(t *testing.T) {
	for _, f := range mysqlFlavors {
		t.Run(f.name, func(t *testing.T) {
			admin := mysqlAdmin(t, f.name, f.env)
			ctx := context.Background()
			drv := f.driver(t.TempDir())
			root := mysqlConn(t, admin, "")
			src := fmt.Sprintf("dbvault_it_%d", time.Now().UnixNano())
			dst := src + "_restored"
			user := "app_" + strconv.FormatInt(time.Now().UnixNano()%1e8, 10)
			password := `p"a\ss'w0rd`
			mysqlExec(t, root, "CREATE DATABASE "+src+" CHARACTER SET utf8mb4",
				fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY 'p\"a\\\\ss''w0rd'", user),
				fmt.Sprintf("GRANT ALL ON %s.* TO '%s'@'%%'", src, user))
			t.Cleanup(func() {
				_, _ = root.Exec("DROP DATABASE IF EXISTS " + src)
				_, _ = root.Exec("DROP DATABASE IF EXISTS " + dst)
				_, _ = root.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user))
			})
			db := mysqlConn(t, admin, src)
			mysqlExec(t, db,
				"CREATE TABLE customers (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(100) NOT NULL, photo BLOB)",
				"CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, customer_id INT, total DECIMAL(10,2), FOREIGN KEY (customer_id) REFERENCES customers(id))",
				"INSERT INTO customers (name, photo) VALUES ('Ann', X'00FF10'), ('Bob ''quoted''', NULL), ('Zoë ✓', X'DEADBEEF')",
				"INSERT INTO orders (customer_id, total) SELECT 1 + (seq % 3), seq / 7 FROM (SELECT a.n * 100 + b.n * 10 + c.n AS seq FROM (SELECT 0 n UNION SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5 UNION SELECT 6 UNION SELECT 7 UNION SELECT 8 UNION SELECT 9) a, (SELECT 0 n UNION SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5 UNION SELECT 6 UNION SELECT 7 UNION SELECT 8 UNION SELECT 9) b, (SELECT 0 n UNION SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5 UNION SELECT 6 UNION SELECT 7 UNION SELECT 8 UNION SELECT 9) c) s",
				fmt.Sprintf("CREATE DEFINER='%s'@'%%' VIEW big_orders AS SELECT * FROM orders WHERE total > 50", user),
				"CREATE TRIGGER orders_bi BEFORE INSERT ON orders FOR EACH ROW SET NEW.total = ROUND(NEW.total, 2)")

			// Back up as the application user (not root).
			target := admin
			target.Username, target.Password, target.Database = user, password, src
			info, err := drv.Inspect(ctx, target)
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			if info.TableCount != 2 || info.Major == 0 {
				t.Fatalf("unexpected server info %+v", info)
			}
			st, _ := storage.NewLocal(t.TempDir(), "")
			pub, ident, err := encryption.GenerateBackupKey()
			if err != nil {
				t.Fatal(err)
			}
			eng := &backups.Engine{Drivers: engine.NewRegistry(drv), VerifyUpload: true}
			key := backups.ObjectKey("it", src, time.Now(), drv.FileExtension(), "zstd", true, "")
			res, err := eng.Run(ctx, backups.Request{Target: target, Storage: st, Key: key, Compression: "zstd", PublicKey: pub, Log: logSink{t}})
			if err != nil {
				t.Fatalf("backup failed: %v", err)
			}
			if res.TableCount != 2 || len(res.Checksum) != 64 || !strings.HasPrefix(res.PGDumpVersion, "mariadb-dump ") {
				t.Fatalf("unexpected result %+v", res)
			}

			open := func() io.ReadCloser {
				path, _, err := backups.DownloadVerified(ctx, st, key, res.Checksum, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				fh, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { fh.Close() })
				a, err := backups.OpenArchive(fh, "zstd", ident)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { a.Close() })
				return a
			}
			tables, err := drv.Tables(ctx, open())
			if err != nil || len(tables) != 2 {
				t.Fatalf("tables %v, %v", tables, err)
			}

			// Restore as root into a new database: the view's definer
			// (the app user) has no rights there.
			if exists, err := drv.DatabaseExists(ctx, admin, dst); err != nil || exists {
				t.Fatalf("exists = %v, %v", exists, err)
			}
			if err := drv.CreateDatabase(ctx, admin, dst); err != nil {
				t.Fatal(err)
			}
			if err := drv.Restore(ctx, admin, dst, open(), engine.RestoreOptions{}, logSink{t}); err != nil {
				t.Fatalf("restore: %v", err)
			}
			check, err := drv.CheckTables(ctx, admin, dst, tables, true)
			if err != nil || check.Found != 2 || check.Rows != 1003 {
				t.Fatalf("check %+v, %v", check, err)
			}
			restored := mysqlConn(t, admin, dst)
			var viewRows int
			if err := restored.QueryRow("SELECT COUNT(*) FROM big_orders").Scan(&viewRows); err != nil {
				t.Fatalf("restored view unusable: %v", err)
			}
			var name string
			var photo []byte
			if err := restored.QueryRow("SELECT name, photo FROM customers WHERE id = 3").Scan(&name, &photo); err != nil || name != "Zoë ✓" || fmt.Sprintf("%X", photo) != "DEADBEEF" {
				t.Fatalf("restored data differs: %q %X %v", name, photo, err)
			}
			var trig int
			if err := restored.QueryRow("SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ?", dst).Scan(&trig); err != nil || trig != 1 {
				t.Fatalf("trigger not restored: %d %v", trig, err)
			}

			// Restoring again over the same database (overwrite) works too.
			if err := drv.Restore(ctx, admin, dst, open(), engine.RestoreOptions{Overwrite: true}, logSink{t}); err != nil {
				t.Fatalf("overwrite restore: %v", err)
			}
			if err := drv.DropDatabase(ctx, admin, dst); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLInspectRejectsWrongFlavor(t *testing.T) {
	for _, f := range mysqlFlavors {
		t.Run(f.name, func(t *testing.T) {
			admin := mysqlAdmin(t, f.name, f.env)
			other := mysql.NewMariaDB("", t.TempDir())
			if f.name == engine.MariaDB {
				other = mysql.NewMySQL("", t.TempDir())
			}
			_, err := other.Inspect(context.Background(), admin)
			if err == nil || !strings.Contains(err.Error(), "choose") {
				t.Fatalf("expected a wrong-engine error, got %v", err)
			}
		})
	}
}

func TestMySQLBackupFailsWithBadCredentials(t *testing.T) {
	for _, f := range mysqlFlavors {
		t.Run(f.name, func(t *testing.T) {
			admin := mysqlAdmin(t, f.name, f.env)
			target := admin
			target.Password, target.Database = "definitely-wrong", "mysql"
			st, _ := storage.NewLocal(t.TempDir(), "")
			eng := &backups.Engine{Drivers: engine.NewRegistry(f.driver(t.TempDir()))}
			_, err := eng.Run(context.Background(), backups.Request{Target: target, Storage: st, Key: "x/backup.sql", Compression: "zstd", Log: logSink{t}})
			if err == nil || !strings.Contains(err.Error(), "authentication failed") {
				t.Fatalf("expected authentication error, got %v", err)
			}
		})
	}
}
