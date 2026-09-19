package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

// SSL modes. They mirror MySQL's --ssl-mode values under the names DBVault
// already uses for PostgreSQL.
var sslModes = []string{"disable", "prefer", "require", "verify-full"}

// open returns a single-connection pool for dbName ("" = no default database).
func open(t engine.Target, dbName string) (*sql.DB, error) {
	cfg := gomysql.NewConfig()
	cfg.User = t.Username
	cfg.Passwd = t.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	cfg.DBName = dbName
	cfg.Timeout = 10 * time.Second
	cfg.AllowNativePasswords = true
	cfg.Collation = "utf8mb4_general_ci"
	switch t.SSLMode {
	case "disable":
		cfg.TLSConfig = "false"
	case "require":
		cfg.TLSConfig = "skip-verify"
	case "verify-full":
		tc := &tls.Config{ServerName: t.Host, MinVersion: tls.VersionTLS12}
		if pem := strings.TrimSpace(t.SSLRootCert); pem != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(pem)) {
				return nil, errors.New("the CA certificate is not valid PEM")
			}
			tc.RootCAs = pool
		}
		cfg.TLS = tc
	default: // prefer
		cfg.TLSConfig = "preferred"
	}
	conn, err := gomysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(1)
	return db, nil
}

// connect opens and pings a connection. In prefer mode a failed TLS
// handshake falls back to an unencrypted connection, as libpq does: MySQL
// 5.7 servers often offer only legacy ciphers that modern clients refuse.
// The returned target records the SSL mode that worked.
func connect(ctx context.Context, t engine.Target, dbName string) (*sql.DB, engine.Target, error) {
	db, err := open(t, dbName)
	if err != nil {
		return nil, t, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = db.PingContext(pingCtx)
	if err == nil {
		return db, t, nil
	}
	db.Close()
	if t.SSLMode != "prefer" || !isTLSError(err) {
		return nil, t, err
	}
	plain := t
	plain.SSLMode = "disable"
	db, err = open(plain, dbName)
	if err != nil {
		return nil, t, err
	}
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, t, err
	}
	return db, plain, nil
}

func isTLSError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:")
}

// effectiveTarget resolves prefer mode to the SSL mode that actually works,
// so the CLI tools (which can't fall back on a failed handshake) use it too.
func effectiveTarget(ctx context.Context, t engine.Target) (engine.Target, error) {
	if t.SSLMode != "prefer" {
		return t, nil
	}
	db, eff, err := connect(ctx, t, "")
	if err != nil {
		return t, FriendlyError(err)
	}
	db.Close()
	return eff, nil
}

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?`)

// parseVersion reads "8.4.3" or "11.4.2-MariaDB-ubu2404".
func parseVersion(v string) (major, num int, short string) {
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return 0, 0, v
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return major, major*10000 + minor*100 + patch, strings.TrimSuffix(m[0], ".")
}

func isMariaDB(version, comment string) bool {
	return strings.Contains(strings.ToLower(version+" "+comment), "mariadb")
}

func (d *Driver) inspect(ctx context.Context, t engine.Target) (engine.ServerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := time.Now()
	db, _, err := connect(ctx, t, t.Database)
	if err != nil {
		return engine.ServerInfo{}, FriendlyError(err)
	}
	defer db.Close()
	var info engine.ServerInfo
	var comment string
	var readOnly int
	err = db.QueryRowContext(ctx, `SELECT VERSION(), @@version_comment, CURRENT_USER(), @@read_only,
		(SELECT COALESCE(SUM(data_length + index_length), 0) FROM information_schema.TABLES WHERE table_schema = DATABASE()),
		(SELECT COUNT(*) FROM information_schema.TABLES WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE')`).
		Scan(&info.FullVersion, &comment, &info.CurrentUser, &readOnly, &info.SizeBytes, &info.TableCount)
	if err != nil {
		return engine.ServerInfo{}, FriendlyError(err)
	}
	if maria := isMariaDB(info.FullVersion, comment); maria != (d.flavor == engine.MariaDB) {
		actual := "MySQL"
		if maria {
			actual = "MariaDB"
		}
		return engine.ServerInfo{}, fmt.Errorf("this server runs %s %s: choose %s as the database type", actual, info.FullVersion, actual)
	}
	if t.SSLMode == "require" || t.SSLMode == "verify-full" {
		var cipher sql.NullString
		var name string
		if err := db.QueryRowContext(ctx, `SHOW SESSION STATUS LIKE 'Ssl_cipher'`).Scan(&name, &cipher); err == nil && cipher.String == "" {
			return engine.ServerInfo{}, errors.New("the server does not support SSL: use ssl mode 'disable' or 'prefer'")
		}
	}
	info.Major, info.VersionNum, info.Version = parseVersion(info.FullVersion)
	if comment != "" {
		info.FullVersion += " (" + comment + ")"
	}
	info.InRecovery = readOnly == 1
	info.LatencyMilli = time.Since(start).Milliseconds()
	return info, nil
}

// FriendlyError converts driver errors into actionable messages that never
// include credentials.
func FriendlyError(err error) error {
	if err == nil {
		return nil
	}
	var myErr *gomysql.MySQLError
	if errors.As(err, &myErr) {
		switch myErr.Number {
		case 1045:
			return errors.New("authentication failed: check the username and password")
		case 1049:
			return errors.New("database does not exist")
		case 1044, 1142, 1227:
			return fmt.Errorf("permission denied: %s", myErr.Message)
		case 1040:
			return errors.New("the server has too many connections")
		case 1130:
			return errors.New("the server rejected this client host (check the user's allowed hosts)")
		}
		return fmt.Errorf("%s (error %d)", myErr.Message, myErr.Number)
	}
	var netErr net.Error
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(msg, "timeout"):
		return errors.New("connection timeout: the server did not respond (check host, port and firewall rules)")
	case strings.Contains(msg, "connection refused"):
		return errors.New("connection refused: nothing is listening on that host and port")
	case strings.Contains(msg, "no such host"):
		return errors.New("host not found: check the hostname")
	case strings.Contains(msg, "TLS requested but server does not support TLS"):
		return errors.New("the server does not support SSL: use ssl mode 'disable' or 'prefer'")
	case strings.Contains(msg, "certificate"):
		return errors.New("TLS certificate verification failed: check the CA certificate and ssl mode")
	}
	return errors.New("could not connect to the database server")
}

// optionFile is a private MySQL option file holding the connection
// settings, so credentials never appear in argv or the environment.
type optionFile struct {
	path   string
	caPath string
}

func (o *optionFile) Close() {
	if o.path != "" {
		_ = os.Remove(o.path)
	}
	if o.caPath != "" {
		_ = os.Remove(o.caPath)
	}
}

// quoteOption quotes a value for a MySQL option file.
func quoteOption(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(v) + `"`
}

// optionFileContent renders the [client] group read by mariadb and
// mariadb-dump. caPath is the CA file for verify-full (may be empty).
func optionFileContent(t engine.Target, caPath string) string {
	var b strings.Builder
	b.WriteString("[client]\n")
	fmt.Fprintf(&b, "user=%s\n", quoteOption(t.Username))
	fmt.Fprintf(&b, "password=%s\n", quoteOption(t.Password))
	fmt.Fprintf(&b, "host=%s\n", quoteOption(t.Host))
	fmt.Fprintf(&b, "port=%d\n", t.Port)
	// Never fall back to a Unix socket for "localhost".
	b.WriteString("protocol=TCP\n")
	switch t.SSLMode {
	case "disable":
		b.WriteString("skip-ssl\n")
	case "verify-full":
		b.WriteString("ssl\nssl-verify-server-cert\n")
		if caPath != "" {
			fmt.Fprintf(&b, "ssl-ca=%s\n", quoteOption(caPath))
		}
	default:
		// prefer, and require (which Inspect enforces before every dump
		// and restore): encrypt when the server supports it.
		b.WriteString("ssl\nskip-ssl-verify-server-cert\n")
	}
	return b.String()
}

func writeOptionFile(t engine.Target, workDir string) (*optionFile, error) {
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	o := &optionFile{}
	if t.SSLMode == "verify-full" && strings.TrimSpace(t.SSLRootCert) != "" {
		p, err := writePrivate(workDir, "ca-*.pem", t.SSLRootCert)
		if err != nil {
			return nil, err
		}
		o.caPath = p
	}
	p, err := writePrivate(workDir, "my-*.cnf", optionFileContent(t, o.caPath))
	if err != nil {
		o.Close()
		return nil, err
	}
	o.path = p
	return o, nil
}

// writePrivate creates a 0600 temp file (os.CreateTemp's default mode).
func writePrivate(dir, pattern, content string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// quoteIdent quotes a MySQL identifier.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
