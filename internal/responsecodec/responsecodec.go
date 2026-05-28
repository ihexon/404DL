package responsecodec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Encryptor interface {
	EncryptString(plaintext string) (string, error)
}

type Decryptor interface {
	DecryptString(encoded string) (string, error)
}

type EncodedBody struct {
	Body        []byte
	ContentType string
	Encrypted   bool
}

const (
	EncryptedResponseHeader        = "X-4DL-Encrypted"
	RequireEncryptedResponseHeader = "X-4DL-Require-Encrypted"
	ResponseEncryptionHeader       = "X-4DL-Encryption"
	ResponseEncryptionValue        = "aes-256-gcm-base64"
	JSONContentType                = "application/json; charset=utf-8"
	EncryptedContentType           = "text/plain; charset=utf-8"
)

func RequireEncryptedResponse(req *http.Request) {
	req.Header.Set(RequireEncryptedResponseHeader, "true")
	req.Header.Set(ResponseEncryptionHeader, ResponseEncryptionValue)
}

func RequiresEncryptedResponse(header http.Header) bool {
	return headerBool(header.Get(RequireEncryptedResponseHeader))
}

func IsEncryptedResponse(header http.Header) bool {
	return headerBool(header.Get(EncryptedResponseHeader))
}

func WriteHeaders(header http.Header, encoded EncodedBody) {
	header.Set("Content-Type", encoded.ContentType)
	if encoded.Encrypted {
		header.Set(EncryptedResponseHeader, "true")
		header.Set(ResponseEncryptionHeader, ResponseEncryptionValue)
	}
}

func EncodeJSON(v any, encryptor Encryptor) (EncodedBody, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return EncodedBody{}, fmt.Errorf("encode json: %w", err)
	}
	if encryptor == nil {
		return EncodedBody{
			Body:        body.Bytes(),
			ContentType: JSONContentType,
		}, nil
	}

	encrypted, err := encryptor.EncryptString(body.String())
	if err != nil {
		return EncodedBody{}, fmt.Errorf("encrypt response: %w", err)
	}
	return EncodedBody{
		Body:        []byte(encrypted + "\n"),
		ContentType: EncryptedContentType,
		Encrypted:   true,
	}, nil
}

func DecodeHTTPBody(body []byte, header http.Header, decryptor Decryptor, requireEncrypted bool) ([]byte, bool, error) {
	encrypted := IsEncryptedResponse(header)
	if requireEncrypted && !encrypted {
		return nil, false, fmt.Errorf("encrypted response required but response was not encrypted")
	}
	if !encrypted {
		return body, false, nil
	}

	if encryption := strings.TrimSpace(header.Get(ResponseEncryptionHeader)); encryption != ResponseEncryptionValue {
		return nil, true, fmt.Errorf("unsupported response encryption %q", encryption)
	}
	return DecryptBody(body, decryptor)
}

func DecryptBody(body []byte, decryptor Decryptor) ([]byte, bool, error) {
	if decryptor == nil {
		return nil, true, fmt.Errorf("encrypted response requires a decryption key")
	}
	plaintext, err := decryptor.DecryptString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, true, fmt.Errorf("decrypt response: %w", err)
	}
	return []byte(plaintext), true, nil
}

func headerBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}
