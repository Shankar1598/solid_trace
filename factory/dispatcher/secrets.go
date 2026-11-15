package dispatcher

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
)

// encryptionKey decodes the key: 32 bytes in standard base64.
func (c Config) encryptionKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(c.EncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New(EnvEncryptionKey + " must be 32 bytes in standard base64 " +
			"(make one with 'openssl rand -base64 32')")
	}
	return key, nil
}

// sealer encrypts each secret in the database with AES-256-GCM. A sealed
// value is the random nonce, then the ciphertext. The purpose (such as
// "connection <id>") is the additional data, so a sealed value that is
// copied to another row does not open.
type sealer struct {
	aead cipher.AEAD
}

func newSealer(key []byte) (*sealer, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{aead: aead}, nil
}

func (s *sealer) seal(purpose string, secret []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(secret)+s.aead.Overhead())
	_, _ = rand.Read(nonce)
	return s.aead.Seal(nonce, nonce, secret, []byte(purpose))
}

func (s *sealer) open(purpose string, sealed []byte) ([]byte, error) {
	if len(sealed) < s.aead.NonceSize() {
		return nil, errors.New("a sealed secret is too short")
	}
	nonce, ciphertext := sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():]
	return s.aead.Open(nil, nonce, ciphertext, []byte(purpose))
}

// keyCheckPurpose and keyCheckValue make the key check: a known value
// sealed at the database's first start. A start with another key cannot
// open it, so the Dispatcher stops before it writes secrets that it could
// not read back with the old key.
const (
	keyCheckPurpose = "key check"
	keyCheckValue   = "the SolidTrace Dispatcher's encryption key"
)

// checkKey seals the key check at the first start, and opens it at each
// later start.
func checkKey(db *sql.DB, s *sealer) error {
	var sealed []byte
	err := db.QueryRow(`SELECT value FROM meta WHERE name = 'key_check'`).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = db.Exec(`INSERT INTO meta (name, value) VALUES ('key_check', ?)`,
			s.seal(keyCheckPurpose, []byte(keyCheckValue)))
		return err
	}
	if err != nil {
		return err
	}
	if plain, err := s.open(keyCheckPurpose, sealed); err != nil || string(plain) != keyCheckValue {
		return fmt.Errorf("%s is not the key that encrypted the secrets in this database: "+
			"start the Dispatcher with the key that it had before", EnvEncryptionKey)
	}
	return nil
}
