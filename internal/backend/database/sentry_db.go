package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"notificator/internal/backend/models"
)

// Encryption key for tokens - should be from environment variable
func getEncryptionKey() []byte {
	key := os.Getenv("NOTIFICATOR_ENCRYPTION_KEY")
	if key == "" {
		// Generate a default key for development - DO NOT USE IN PRODUCTION
		key = "dev-encryption-key-32-bytes-long"
	}
	// Ensure key is 32 bytes for AES-256
	keyBytes := []byte(key)
	if len(keyBytes) < 32 {
		// Pad with zeros
		padded := make([]byte, 32)
		copy(padded, keyBytes)
		return padded
	}
	return keyBytes[:32]
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