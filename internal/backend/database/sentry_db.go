package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"

	"notificator/internal/backend/models"
)

// EncryptionKeyEnvVar is the environment variable holding the AES-256 key
// used to encrypt Sentry personal tokens at rest.
const EncryptionKeyEnvVar = "NOTIFICATOR_ENCRYPTION_KEY"

// devDefaultEncryptionKey is the placeholder key shipped in docker-compose.yml
// for zero-friction local dev/test. It must never be used in production.
const devDefaultEncryptionKey = "badbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadb"

// ValidateEncryptionKey checks that NOTIFICATOR_ENCRYPTION_KEY is set to 64
// lowercase hex characters (32 raw bytes), returning an error naming the
// variable and the generation command otherwise. It also warns loudly when
// the key matches the known dev default, since that means a deployment is
// running with a publicly-known, non-secret encryption key.
func ValidateEncryptionKey() error {
	_, err := encryptionKeyFromEnv()
	if err != nil {
		return err
	}
	if os.Getenv(EncryptionKeyEnvVar) == devDefaultEncryptionKey {
		log.Printf("WARNING: %s is set to the known dev/test default from docker-compose.yml. This key is public and MUST NOT be used to protect real secrets — generate your own with `openssl rand -hex 32`.", EncryptionKeyEnvVar)
	}
	return nil
}

func encryptionKeyFromEnv() ([]byte, error) {
	key := os.Getenv(EncryptionKeyEnvVar)
	if len(key) != 64 {
		return nil, fmt.Errorf("%s must be set to 64 lowercase hex characters (32 bytes); generate one with `openssl rand -hex 32`", EncryptionKeyEnvVar)
	}
	keyBytes, err := hex.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("%s must be 64 lowercase hex characters (32 bytes); generate one with `openssl rand -hex 32`", EncryptionKeyEnvVar)
	}
	return keyBytes, nil
}

func getEncryptionKey() []byte {
	key, err := encryptionKeyFromEnv()
	if err != nil {
		// ValidateEncryptionKey is called at startup, so this can only be
		// reached if the environment changed after the process started.
		panic(err)
	}
	return key
}

// Encrypt encrypts plaintext using AES
func encrypt(plaintext string) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES
func decrypt(ciphertext string) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], string(data[nonceSize:])
	plaintext, err := gcm.Open(nil, nonce, []byte(ciphertext), nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// GetUserSentryConfig retrieves a user's Sentry configuration
func (gdb *GormDB) GetUserSentryConfig(userID string) (*models.UserSentryConfig, error) {
	var config models.UserSentryConfig
	err := gdb.db.Where("user_id = ?", userID).First(&config).Error
	if err != nil {
		return nil, err
	}

	// Decrypt the personal token
	if config.PersonalToken != "" {
		decrypted, err := decrypt(config.PersonalToken)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt personal token: %w", err)
		}
		config.PersonalToken = decrypted
	}

	return &config, nil
}

// SaveUserSentryConfig saves or updates a user's Sentry configuration
func (gdb *GormDB) SaveUserSentryConfig(userID string, personalToken, baseURL string) error {
	// Encrypt the personal token
	encrypted, err := encrypt(personalToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt personal token: %w", err)
	}

	config := models.UserSentryConfig{
		UserID:        userID,
		PersonalToken: encrypted,
		SentryBaseURL: baseURL,
	}

	// Use Upsert - update if exists, create if not
	result := gdb.db.Where("user_id = ?", userID).Assign(config).FirstOrCreate(&config)
	return result.Error
}

// DeleteUserSentryConfig removes a user's Sentry configuration
func (gdb *GormDB) DeleteUserSentryConfig(userID string) error {
	return gdb.db.Where("user_id = ?", userID).Delete(&models.UserSentryConfig{}).Error
}