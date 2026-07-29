package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWT_IssueAndVerify(t *testing.T) {
	svc := NewJWTService("test-secret")
	claims := Claims{UserID: "u1", Email: "admin@example.com", Name: "Admin", Role: RoleAdmin}

	token, err := svc.Issue(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	got, err := svc.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "u1", got.UserID)
	assert.Equal(t, RoleAdmin, got.Role)
}

func TestJWT_InvalidToken(t *testing.T) {
	svc := NewJWTService("secret")
	_, err := svc.Verify("not-a-token")
	assert.Error(t, err)
}

func TestJWT_WrongSecret(t *testing.T) {
	svc1 := NewJWTService("secret1")
	svc2 := NewJWTService("secret2")
	token, _ := svc1.Issue(Claims{UserID: "u1", Role: RoleViewer})
	_, err := svc2.Verify(token)
	assert.Error(t, err)
}

func TestUserStore_CRUD(t *testing.T) {
	s := NewUserStore()
	hash, _ := HashPassword("password123")
	err := s.CreateUser(&User{Email: "test@example.com", Name: "Test", Role: RoleAdmin, PasswordHash: hash})
	require.NoError(t, err)

	u, ok := s.GetByEmail("test@example.com")
	require.True(t, ok)
	assert.Equal(t, RoleAdmin, u.Role)
	assert.True(t, CheckPassword(u.PasswordHash, "password123"))
	assert.False(t, CheckPassword(u.PasswordHash, "wrong"))
}

func TestUserStore_RefreshToken(t *testing.T) {
	s := NewUserStore()
	s.CreateUser(&User{ID: "u1", Email: "a@b.com", Role: RoleViewer})

	tok := s.IssueRefreshToken("u1")
	assert.NotEmpty(t, tok)

	uid, valid := s.ValidateRefreshToken(tok)
	assert.True(t, valid)
	assert.Equal(t, "u1", uid)

	// Token consumed (rotated)
	_, valid = s.ValidateRefreshToken(tok)
	assert.False(t, valid, "refresh token must be single-use")
}

func TestRole_CanWrite(t *testing.T) {
	assert.False(t, RoleViewer.CanWrite())
	assert.True(t, RoleEditor.CanWrite())
	assert.True(t, RoleAdmin.CanWrite())
	assert.True(t, RoleOwner.CanWrite())
}
