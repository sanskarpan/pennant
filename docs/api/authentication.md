# Authentication

Pennant uses JWT (HS256) tokens for the management API and SDK keys for the SDK endpoints.

## Login

Exchange credentials for an access token and a refresh token.

```
POST /auth/login
```

**Rate limit:** 5 requests per minute per IP address.

**Request:**

```bash
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{
    "email": "admin@pennant.local",
    "password": "admin"
  }'
```

**Response `200 OK`:**

```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refreshToken": "7f3b2a1c9e4d5f6a8b2c3d4e5f6a7b8c..."
}
```

- `accessToken` — JWT, valid for **1 hour**. Include in subsequent API requests as `Authorization: Bearer <token>`.
- `refreshToken` — opaque token, valid for **7 days**. Single-use; rotate it before it expires.

**Response `401 Unauthorized`:**

```json
{"error": "invalid credentials"}
```

## Refresh tokens

Exchange a refresh token for a new access/refresh token pair. Refresh tokens are **single-use** — the old token is invalidated when a new one is issued.

```
POST /auth/refresh
```

```bash
curl -X POST http://localhost:8080/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{
    "refreshToken": "7f3b2a1c9e4d5f6a8b2c3d4e5f6a7b8c..."
  }'
```

**Response `200 OK`:**

```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refreshToken": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d..."
}
```

## Get current user

Returns the authenticated user's profile and role.

```
GET /auth/me
```

```bash
curl http://localhost:8080/auth/me \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Response `200 OK`:**

```json
{
  "id": "usr_01HXYZ",
  "email": "admin@pennant.local",
  "role": "owner"
}
```

## RBAC roles

| Role | Can do |
|---|---|
| `owner` | Everything, including deleting projects and managing users |
| `admin` | Create/edit/delete flags, segments, experiments, environments |
| `editor` | Create/edit flags and segments; cannot delete or manage environments |
| `viewer` | Read-only access to all resources |

All management API endpoints (`/api/v1/...`) require a valid JWT. The required role is checked per endpoint.

## SDK key authentication

SDK endpoints (`/sdk/v1/...`) use SDK keys instead of JWTs. Pass the SDK key as a Bearer token:

```bash
curl http://localhost:8080/sdk/v1/snapshot \
  -H "Authorization: Bearer sdk-server-default-prod"
```

SDK keys are scoped to a project + environment pair and do not grant access to the management API.

## Token storage

- Store access tokens in memory (not in `localStorage` or cookies, unless you implement CSRF protection).
- Refresh tokens should be stored in an `HttpOnly` cookie or a secure persistent store.
- Never log or expose tokens in URLs, query parameters, or error messages.

## Legacy token auth

If `PENNANT_ADMIN_TOKEN` is set (not recommended for new deployments), any request with `Authorization: Bearer <token>` matching that value receives `owner` access without JWT validation. This is provided for backward compatibility and automated scripts that predate the JWT implementation. Prefer creating a service account with a long-lived SDK key or a dedicated API user instead.
