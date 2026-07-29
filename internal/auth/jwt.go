package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const accessTokenTTL = time.Hour

type JWTService struct {
	secret []byte
}

func NewJWTService(secret string) *JWTService {
	if secret == "" {
		slog.Warn("PENNANT_JWT_SECRET is not set — using insecure dev default. NEVER use this in production.")
		secret = "dev-insecure-secret-change-in-production"
	}
	return &JWTService{secret: []byte(secret)}
}

type jwtClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  Role   `json:"role"`
	jwt.RegisteredClaims
}

func (s *JWTService) Issue(c Claims) (string, error) {
	claims := jwtClaims{
		Email: c.Email,
		Name:  c.Name,
		Role:  c.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.UserID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.secret)
}

func (s *JWTService) Verify(tokenStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := t.Claims.(*jwtClaims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token claims")
	}
	return &Claims{
		UserID: c.Subject,
		Email:  c.Email,
		Name:   c.Name,
		Role:   c.Role,
	}, nil
}
