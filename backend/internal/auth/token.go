// Package auth provides lightweight password hashing and signed session
// tokens for Aruzor. Tokens are HMAC-SHA256 signed JSON payloads (no
// external JWT dependency) that carry the user id, email and role.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("gecersiz oturum tokeni")
	ErrExpiredToken = errors.New("oturum tokeninin suresi dolmus")
)

type Claims struct {
	UserID string `json:"sub"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Exp    int64  `json:"exp"`
}

type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenIssuer(secret string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), ttl: ttl}
}

func (i *TokenIssuer) Issue(userID, email, role string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		Exp:    time.Now().Add(i.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := i.sign(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func (i *TokenIssuer) Verify(token string) (*Claims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidToken
	}
	encodedPayload, signature := parts[0], parts[1]

	expected := i.sign(encodedPayload)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) != 1 {
		return nil, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, ErrInvalidToken
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().Unix() > claims.Exp {
		return nil, ErrExpiredToken
	}
	return &claims, nil
}

func (i *TokenIssuer) sign(data string) string {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
