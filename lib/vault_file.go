package vaulted

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"math/big"

	"golang.org/x/crypto/pbkdf2"
)

type VaultFile struct {
	Key *VaultKey `json:"key"`

	Method     string  `json:"method"`
	Details    Details `json:"details,omitempty"`
	Ciphertext []byte  `json:"ciphertext"`
}

type VaultKey struct {
	Method  string  `json:"method"`
	Details Details `json:"details"`
}

func newVaultKey(previous *VaultKey) *VaultKey {
	var method string
	var details Details

	// Copy previous key details, if present
	if previous != nil {
		method = previous.Method
		details = previous.Details.Clone()
	} else {
		method = "pbkdf2-sha512"
		details = make(Details)
	}

	// Adjust cost parameters
	switch method {
	case "pbkdf2-sha512":
		details.SetInt("iterations", adjustIterations(details.Int("iterations")))

		salt := make([]byte, 32)
		_, err := rand.Read(salt)
		if err != nil {
			return nil
		}
		details.SetBytes("salt", salt)
	}

	return &VaultKey{
		Method:  method,
		Details: details,
	}
}

func (vk *VaultKey) key(password string, keyLength int) ([]byte, error) {
	switch vk.Method {
	case "pbkdf2-sha512":
		iterations := vk.Details.Int("iterations")
		salt := vk.Details.Bytes("salt")
		if iterations == 0 || len(salt) == 0 {
			return nil, ErrInvalidKeyConfig
		}
		return pbkdf2.Key([]byte(password), salt, iterations, keyLength, sha512.New), nil
	}

	return nil, fmt.Errorf("Invalid key derivation method: %s", vk.Method)
}

func adjustIterations(iterations int) int {
	if iterations < 65536 {
		r, err := rand.Int(rand.Reader, big.NewInt(32768))
		if err != nil {
			return 65536
		}

		return 65536 + int(r.Int64())
	}

	if iterations > 1048576 {
		r, err := rand.Int(rand.Reader, big.NewInt(32768))
		if err != nil {
			return 1048576
		}

		return 1048576 - int(r.Int64())
	}

	r, err := rand.Int(rand.Reader, big.NewInt(256))
	if err != nil {
		return iterations + 1
	}

	return iterations + int(r.Int64()) - 32
}

type Details map[string]interface{}

func (d Details) Clone() Details {
	newKeyDetails := make(Details)
	for k, v := range d {
		newKeyDetails[k] = v
	}
	return newKeyDetails
}

func (d Details) Int(name string) int {
	if v, ok := d[name].(int); ok {
		return v
	}
	if v, ok := d[name].(int64); ok {
		return int(v)
	}
	if v, ok := d[name].(float64); ok {
		return int(v)
	}
	return 0
}

func (d Details) SetInt(name string, value int) {
	d[name] = value
}

func (d Details) String(name string) string {
	v, _ := d[name].(string)
	return v
}

func (d Details) SetString(name string, value string) {
	d[name] = value
}

func (d Details) Bytes(name string) []byte {
	b, err := base64.StdEncoding.DecodeString(d.String(name))
	if err != nil {
		return nil
	}
	return b
}

func (d Details) SetBytes(name string, value []byte) {
	d[name] = base64.StdEncoding.EncodeToString(value)
}