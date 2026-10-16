// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/mail"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Version                                                                                             string
	DatabaseURL, Address, AdminEmail, AdminPassword, EncryptionKey, SourceURL, PublicURL, SourceArchive string
	SecureCookies                                                                                       bool
}

func EnvConfig() Config {
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Address: address, AdminEmail: os.Getenv("ADMIN_EMAIL"), AdminPassword: os.Getenv("ADMIN_PASSWORD"), EncryptionKey: os.Getenv("APP_ENCRYPTION_KEY"), SourceURL: os.Getenv("SOURCE_URL"), PublicURL: os.Getenv("PUBLIC_URL"), SourceArchive: os.Getenv("SOURCE_ARCHIVE"), SecureCookies: os.Getenv("COOKIE_SECURE") != "false"}
	if c.SourceArchive != "" && c.SourceURL == "" {
		c.SourceURL = strings.TrimRight(c.PublicURL, "/") + "/source"
	}
	return c
}
func (c Config) validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required (PostgreSQL only)")
	}
	a, e := mail.ParseAddress(c.AdminEmail)
	if e != nil || a.Address != c.AdminEmail || len(c.AdminPassword) < 12 || len(c.AdminPassword) > 72 {
		return errors.New("ADMIN_EMAIL and ADMIN_PASSWORD (12–72 bytes) are required")
	}
	if c.SourceArchive != "" {
		f, e := os.Stat(c.SourceArchive)
		if e != nil || !f.Mode().IsRegular() {
			return errors.New("SOURCE_ARCHIVE must point to a readable regular source archive")
		}
	}
	for _, v := range []string{c.SourceURL, c.PublicURL} {
		u, e := url.Parse(v)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("SOURCE_URL and PUBLIC_URL must be absolute HTTP(S) URLs without credentials, query or fragment")
		}
	}
	u, _ := url.Parse(c.PublicURL)
	if u.Path != "" && u.Path != "/" {
		return errors.New("PUBLIC_URL must be an origin without a path prefix")
	}
	return nil
}
func newCipher(key string) (cipher.AEAD, error) {
	b, e := base64.StdEncoding.DecodeString(key)
	if e != nil || len(b) != 32 {
		return nil, errors.New("APP_ENCRYPTION_KEY must be base64 of exactly 32 bytes")
	}
	block, e := aes.NewCipher(b)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func seal(a cipher.AEAD, plain string) []byte {
	n := make([]byte, a.NonceSize())
	if _, e := rand.Read(n); e != nil {
		panic(e)
	}
	return a.Seal(n, n, []byte(plain), []byte("s3webui-credential-v1"))
}
func unseal(a cipher.AEAD, b []byte) (string, error) {
	if len(b) < a.NonceSize() {
		return "", errors.New("invalid ciphertext")
	}
	p, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("s3webui-credential-v1"))
	return string(p), e
}
