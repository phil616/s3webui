// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCredentialEncryption(t *testing.T) {
	a, e := newCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if e != nil {
		t.Fatal(e)
	}
	b := seal(a, "secret 密钥")
	if bytes.Contains(b, []byte("secret")) {
		t.Fatal("plaintext stored")
	}
	p, e := unseal(a, b)
	if e != nil || p != "secret 密钥" {
		t.Fatalf("decrypt: %q %v", p, e)
	}
	b[len(b)-1] ^= 1
	if _, e = unseal(a, b); e == nil {
		t.Fatal("tampering accepted")
	}
	if _, e = unseal(a, []byte{1}); e == nil {
		t.Fatal("short ciphertext accepted")
	}
}
func TestCopySourcePreservesKey(t *testing.T) {
	got := copySource("bucket", "中文/a+b #?%//../file", "null")
	want := "bucket%2F%E4%B8%AD%E6%96%87%2Fa%2Bb%20%23%3F%25%2F%2F..%2Ffile?versionId=null"
	if got != want {
		t.Fatalf("%s != %s", got, want)
	}
}
func TestConfigFailsClosed(t *testing.T) {
	c := Config{DatabaseURL: "postgres://localhost/test", AdminEmail: "admin@example.com", AdminPassword: strings.Repeat("x", 12), SourceURL: "https://example.com/source", PublicURL: "https://example.com"}
	if e := c.validate(); e != nil {
		t.Fatal(e)
	}
	c.SourceURL = ""
	if c.validate() == nil {
		t.Fatal("missing source URL accepted")
	}
	if _, e := newCipher("not-a-key"); e == nil {
		t.Fatal("invalid encryption key accepted")
	}
}
