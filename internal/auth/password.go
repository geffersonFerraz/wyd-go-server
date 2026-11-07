// Package auth hashes account secrets. The C++ server kept them in clear
// text in the XML; here they are argon2id PHC strings.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP-recommended minimum for argon2id: 19 MiB, 2 iterations, 1 lane.
const (
	memoryKiB = 19 * 1024
	time      = 2
	threads   = 1
	keyLen    = 32
	saltLen   = 16
)

var errFormat = errors.New("auth: malformed hash")

// Hash returns an argon2id PHC string for secret.
func Hash(secret string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(secret), salt, time, memoryKiB, threads, keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, time, threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether secret matches hash. It honours the parameters
// stored in the hash, so they can be raised later without a migration.
func Verify(hash, secret string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errFormat
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errFormat
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errFormat
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errFormat
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, errFormat
	}
	got := argon2.IDKey([]byte(secret), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// AccountStatus is the state the C++ server encoded as the first character
// of the stored password.
type AccountStatus int16

const (
	StatusNormal    AccountStatus = 0
	StatusBlocked   AccountStatus = 1 // '@'
	StatusDefective AccountStatus = 2 // '_'
	StatusDisabled  AccountStatus = 3 // '#'
)

// SplitLegacyPassword separates the status prefix from a stored password.
func SplitLegacyPassword(stored string) (AccountStatus, string) {
	if stored == "" {
		return StatusNormal, ""
	}
	switch stored[0] {
	case '@':
		return StatusBlocked, stored[1:]
	case '_':
		return StatusDefective, stored[1:]
	case '#':
		return StatusDisabled, stored[1:]
	}
	return StatusNormal, stored
}
