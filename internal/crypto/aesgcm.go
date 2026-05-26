package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type StringEncryptor struct {
	gcm cipher.AEAD
}

func NewStringEncryptor(key string) (*StringEncryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("MVDL_CRYKEY must be 32 bytes for AES-256, got %d", len(key))
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create aes-gcm: %w", err)
	}

	return &StringEncryptor{gcm: gcm}, nil
}

func (e *StringEncryptor) EncryptString(plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (e *StringEncryptor) DecryptString(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(data) < e.gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce := data[:e.gcm.NonceSize()]
	ciphertext := data[e.gcm.NonceSize():]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}

	return string(plaintext), nil
}

func (e *StringEncryptor) GenKey() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}

	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}

	return string(buf), nil
}

func StringMasker(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:len(value)-4] + "****"
}
