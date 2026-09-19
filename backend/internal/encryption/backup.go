package encryption

import (
	"context"
	"fmt"
	"io"

	"filippo.io/age"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbvault/dbvault/backend/internal/db"
)

// GenerateBackupKey creates a new age X25519 key pair.
func GenerateBackupKey() (publicKey, identity string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.Recipient().String(), id.String(), nil
}

// EncryptWriter returns a writer that age-encrypts everything written to it
// into dst. Close must be called to flush the final authenticated chunk.
func EncryptWriter(dst io.Writer, publicKey string) (io.WriteCloser, error) {
	r, err := age.ParseX25519Recipient(publicKey)
	if err != nil {
		return nil, fmt.Errorf("parse recipient: %w", err)
	}
	return age.Encrypt(dst, r)
}

// DecryptReader returns a reader that authenticates and decrypts src.
// Tampering is detected per 64 KiB chunk and surfaces as a read error.
func DecryptReader(src io.Reader, identity string) (io.Reader, error) {
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	return age.Decrypt(src, id)
}

// Key is an organization's backup encryption key.
type Key struct {
	ID        string
	PublicKey string
}

// KeyStore manages per-organization backup keys. Private keys are sealed
// with the master key and only unsealed in memory when needed.
type KeyStore struct {
	pool   *pgxpool.Pool
	sealer *Sealer
}

func NewKeyStore(pool *pgxpool.Pool, sealer *Sealer) *KeyStore {
	return &KeyStore{pool: pool, sealer: sealer}
}

func keyAAD(orgID string) string { return "encryption_key:" + orgID }

// CreateForOrg generates and stores an active key for a new organization.
func (k *KeyStore) CreateForOrg(ctx context.Context, q db.DB, orgID string) error {
	pub, ident, err := GenerateBackupKey()
	if err != nil {
		return err
	}
	sealed, err := k.sealer.SealString(ident, keyAAD(orgID))
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO encryption_keys (organization_id, public_key, private_key_encrypted) VALUES ($1, $2, $3)`,
		orgID, pub, sealed)
	return err
}

// Active returns the organization's active key.
func (k *KeyStore) Active(ctx context.Context, orgID string) (Key, error) {
	var key Key
	err := k.pool.QueryRow(ctx, `SELECT id, public_key FROM encryption_keys WHERE organization_id = $1 AND active`, orgID).
		Scan(&key.ID, &key.PublicKey)
	return key, err
}

// PublicKey returns the recipient for keyID (scoped to orgID).
func (k *KeyStore) PublicKey(ctx context.Context, orgID, keyID string) (string, error) {
	var pub string
	err := k.pool.QueryRow(ctx, `SELECT public_key FROM encryption_keys WHERE id = $1 AND organization_id = $2`, keyID, orgID).Scan(&pub)
	return pub, err
}

// Identity unseals the private key for keyID (scoped to orgID).
func (k *KeyStore) Identity(ctx context.Context, orgID, keyID string) (string, error) {
	var sealed string
	err := k.pool.QueryRow(ctx, `SELECT private_key_encrypted FROM encryption_keys WHERE id = $1 AND organization_id = $2`, keyID, orgID).
		Scan(&sealed)
	if err != nil {
		return "", err
	}
	return k.sealer.OpenString(sealed, keyAAD(orgID))
}

// ActiveIdentity returns the active key id and its unsealed identity (for recovery-key export).
func (k *KeyStore) ActiveIdentity(ctx context.Context, orgID string) (Key, string, error) {
	key, err := k.Active(ctx, orgID)
	if err != nil {
		return Key{}, "", err
	}
	id, err := k.Identity(ctx, orgID, key.ID)
	return key, id, err
}
