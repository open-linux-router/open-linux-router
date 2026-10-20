package system

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const passwordIterations = 600000

func HashPassword(value string) (*Password, error) {
	if utf8.RuneCountInString(value) < 12 || len(value) > 1024 {
		return nil, fmt.Errorf("password must contain at least 12 characters and at most 1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := pbkdf2.Key(sha256.New, value, salt, passwordIterations, 32)
	if err != nil {
		return nil, err
	}
	encode := base64.RawStdEncoding.EncodeToString
	return &Password{Hash: fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations, encode(salt), encode(key))}, nil
}

func (p *Password) Matches(value string) bool {
	if p == nil || len(value) > 1024 {
		return false
	}
	parts := strings.Split(p.Hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < passwordIterations || iterations > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, value, salt, iterations, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// ValidHash checks a stored credential without performing password hashing.
// Restore must reject a malformed hash before replacing working admin access.
func (p *Password) ValidHash() bool {
	if p == nil {
		return false
	}
	parts := strings.Split(p.Hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < passwordIterations || iterations > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[3])
	return err == nil && len(key) == 32
}
