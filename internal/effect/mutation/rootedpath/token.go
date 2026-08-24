package rootedpath

import (
	"crypto/sha256"
	"encoding/binary"
)

func identityTokenFromValues(domain string, values ...uint64) identityToken {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	var encoded [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(encoded[:], value)
		_, _ = hash.Write(encoded[:])
	}
	var token identityToken
	copy(token[:], hash.Sum(nil))
	return token
}

func identityTokenFromBytes(domain string, values ...[]byte) identityToken {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	var length [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(value)
	}
	var token identityToken
	copy(token[:], hash.Sum(nil))
	return token
}
