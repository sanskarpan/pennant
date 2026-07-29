package sdkauth

import (
	"strings"
	"sync"
)

// SDKKeyRecord holds metadata about a registered SDK key.
type SDKKeyRecord struct {
	Value      string
	ProjectKey string
	EnvKey     string
	Type       string // "server", "client", "mobile"
	Revoked    bool
}

// Authenticator validates SDK keys and maps them to environments.
type Authenticator struct {
	mu   sync.RWMutex
	keys map[string]*SDKKeyRecord // value -> record
}

func NewAuthenticator() *Authenticator {
	return &Authenticator{keys: make(map[string]*SDKKeyRecord)}
}

// Register adds or updates an SDK key record.
func (a *Authenticator) Register(rec *SDKKeyRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.keys[rec.Value] = rec
}

// Revoke marks an SDK key as revoked.
func (a *Authenticator) Revoke(keyValue string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if rec, ok := a.keys[keyValue]; ok {
		rec.Revoked = true
	}
}

// ValidateSDKKey validates a key against a specific environment.
func (a *Authenticator) ValidateSDKKey(keyValue, envKey string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	rec, ok := a.keys[keyValue]
	if !ok || rec.Revoked {
		return false
	}
	return rec.EnvKey == envKey
}

// Lookup returns the full record for a key, or nil if not found/revoked.
func (a *Authenticator) Lookup(keyValue string) *SDKKeyRecord {
	a.mu.RLock()
	defer a.mu.RUnlock()
	rec, ok := a.keys[keyValue]
	if !ok || rec.Revoked {
		return nil
	}
	return rec
}

// IsClientKey returns true if the key is client-side (restricted payload).
func (a *Authenticator) IsClientKey(keyValue string) bool {
	rec := a.Lookup(keyValue)
	if rec == nil {
		return false
	}
	return rec.Type == "client" || rec.Type == "mobile"
}

// ExtractBearer extracts the Bearer token from an Authorization header.
func ExtractBearer(authHeader string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(authHeader, prefix) {
		return authHeader[len(prefix):]
	}
	return ""
}
