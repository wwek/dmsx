package engine

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// BackupUser is a login that can take a backup and cannot change data.
type BackupUser struct {
	Username string
	Password string
}

// CreateBackupUser creates that login on the server src points at, signing in
// as src. With replication, a PostgreSQL role gets REPLICATION for
// pg_basebackup instead of SELECT on the database. The statements go to the
// client tool on stdin, so the new password is not a process argument.
func CreateBackupUser(ctx context.Context, src Source, replication bool) (BackupUser, error) {
	user, err := newBackupUser()
	if err != nil {
		return BackupUser{}, err
	}
	switch src.Engine {
	case MySQL, MariaDB:
		err = createMySQLUser(ctx, src, user)
	case PostgreSQL:
		err = createPostgresRole(ctx, src, user, replication)
	case MongoDB:
		err = createMongoUser(ctx, src, user)
	default:
		err = fmt.Errorf("unknown engine %q", src.Engine)
	}
	if err != nil {
		return BackupUser{}, fmt.Errorf("%s", redact(redact(err.Error(), user.Password), src.Password))
	}
	return user, nil
}

func newBackupUser() (BackupUser, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return BackupUser{}, err
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return BackupUser{}, err
	}
	password := make([]byte, len(raw))
	for i, b := range raw {
		password[i] = alphabet[int(b)%len(alphabet)]
	}
	return BackupUser{Username: "dmsx_ro_" + hex.EncodeToString(suffix[:]), Password: string(password)}, nil
}

func createMySQLUser(ctx context.Context, src Source, user BackupUser) error {
	if strings.ContainsAny(src.Database, "`'\\") {
		return fmt.Errorf("database name must be a plain identifier")
	}
	bin := "mysql"
	maria := src.Engine == MariaDB
	if maria {
		bin = "mariadb"
	}
	cnf, err := writeMyCNF(src, maria)
	if err != nil {
		return err
	}
	defer cnf.remove()
	statements := fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s';\n"+
		"GRANT SELECT, SHOW VIEW, LOCK TABLES, TRIGGER, EVENT ON `%s`.* TO '%s'@'%%';\n"+
		"FLUSH PRIVILEGES;\n",
		user.Username, user.Password, src.Database, user.Username)
	return run(ctx, bin, []string{cnf.arg()}, nil, strings.NewReader(statements), io.Discard)
}

func createPostgresRole(ctx context.Context, src Source, user BackupUser, replication bool) error {
	pass, err := writePGPass(src)
	if err != nil {
		return err
	}
	defer pass.remove()
	verifier, err := scramVerifier(user.Password)
	if err != nil {
		return err
	}
	role := pgIdent(user.Username)
	var statements string
	if replication {
		statements = fmt.Sprintf("CREATE ROLE %s LOGIN REPLICATION PASSWORD '%s';\n", role, verifier)
	} else {
		statements = fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s';\n", role, verifier) +
			fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;\n", pgIdent(src.Database), role) +
			fmt.Sprintf(`DO $dmsx$
DECLARE s text;
BEGIN
  FOR s IN SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg\_%%' AND nspname <> 'information_schema' LOOP
    EXECUTE format('GRANT USAGE ON SCHEMA %%I TO %%I', s, %[1]s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %%I TO %%I', s, %[1]s);
    EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %%I TO %%I', s, %[1]s);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %%I GRANT SELECT ON TABLES TO %%I', s, %[1]s);
    EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %%I GRANT SELECT ON SEQUENCES TO %%I', s, %[1]s);
  END LOOP;
END
$dmsx$;
`, pgLiteral(user.Username))
	}
	args := []string{"--no-password", "--quiet", "-v", "ON_ERROR_STOP=1", "--single-transaction",
		"-h", src.Host, "-p", strconv.Itoa(src.Port), "-U", src.User, "-d", src.Database}
	env := []string{"PGPASSFILE=" + pass.path}
	if src.SSL {
		env = append(env, "PGSSLMODE=require")
	}
	return run(ctx, "psql", args, env, strings.NewReader(statements), io.Discard)
}

// scramVerifier is the SCRAM-SHA-256 secret PostgreSQL stores for a password.
// Sending it instead of the password keeps the password out of server logs.
func scramVerifier(password string) (string, error) {
	const iterations = 4096
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", err
	}
	clientKey := hmacSHA256(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	serverKey := hmacSHA256(salted, "Server Key")
	b64 := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, b64(salt), b64(storedKey[:]), b64(serverKey)), nil
}

func hmacSHA256(key []byte, message string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

func pgIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func pgLiteral(text string) string { return `'` + strings.ReplaceAll(text, `'`, `''`) + `'` }

// createMongoUser signs in from inside the mongosh script, so neither password
// is a process argument.
func createMongoUser(ctx context.Context, src Source, user BackupUser) error {
	quote := func(text string) string {
		encoded, _ := json.Marshal(text)
		return string(encoded)
	}
	address := "mongodb://" + src.Host + ":" + strconv.Itoa(src.Port) + "/"
	script := fmt.Sprintf(`try {
  const conn = new Mongo(%s);
  const home = conn.getDB(%s);
  home.auth(%s, %s);
  home.createUser({user: %s, pwd: %s, roles: [{role: "backup", db: "admin"}, {role: "read", db: %s}]});
} catch (e) {
  print(e.message);
  quit(1);
}
`, quote(address), quote(src.Database), quote(src.User), quote(src.Password),
		quote(user.Username), quote(user.Password), quote(src.Database))
	return run(ctx, "mongosh", []string{"--nodb", "--quiet"}, nil, strings.NewReader(script), io.Discard)
}
