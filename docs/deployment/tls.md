# TLS Configuration

Pennant supports three TLS modes. The mode is selected by which environment variables are set, in priority order:

1. **Automatic (Let's Encrypt)** — set `TLS_AUTO_DOMAIN`
2. **Manual certificate** — set `TLS_CERT_FILE` + `TLS_KEY_FILE`
3. **Plain HTTP** — set neither (development only)

## Mode 1: Automatic Let's Encrypt

Pennant uses `golang.org/x/crypto/acme/autocert` to obtain and renew TLS certificates automatically via ACME HTTP-01 challenge.

```bash
TLS_AUTO_DOMAIN=flags.example.com ./pennant-server
```

Or in Docker:

```yaml
environment:
  TLS_AUTO_DOMAIN: flags.example.com
ports:
  - "80:80"
  - "443:443"
volumes:
  - autocert-cache:/root/.cache/autocert
```

**Requirements:**
- The domain must resolve to the server's public IP.
- Port 80 must be reachable from the internet (for ACME HTTP-01 challenge).
- Port 443 is where the server listens.
- A writable directory for the certificate cache (the server writes to `/root/.cache/autocert` by default; mount a persistent volume).

**How it works:**
1. On first start, Pennant requests a certificate from Let's Encrypt.
2. ACME HTTP-01 challenge: Let's Encrypt makes a request to `http://flags.example.com/.well-known/acme-challenge/<token>`. Pennant serves this response automatically.
3. Let's Encrypt issues the certificate, valid for 90 days.
4. Pennant automatically renews the certificate before expiry.

**Docker Compose example:**

```yaml
services:
  pennant:
    build: .
    environment:
      PENNANT_JWT_SECRET: ${PENNANT_JWT_SECRET}
      SQLITE_PATH: /data/pennant.db
      TLS_AUTO_DOMAIN: flags.example.com
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - pennant-data:/data
      - autocert-cache:/root/.cache/autocert
    restart: unless-stopped

volumes:
  pennant-data:
  autocert-cache:
```

## Mode 2: Manual certificate

Provide an existing PEM-encoded certificate and private key:

```bash
TLS_CERT_FILE=/etc/ssl/certs/flags.example.com.crt \
TLS_KEY_FILE=/etc/ssl/private/flags.example.com.key \
./pennant-server
```

The server listens on `:443` when TLS is enabled.

**Docker Compose example with certificate files:**

```yaml
services:
  pennant:
    build: .
    environment:
      PENNANT_JWT_SECRET: ${PENNANT_JWT_SECRET}
      SQLITE_PATH: /data/pennant.db
      TLS_CERT_FILE: /certs/tls.crt
      TLS_KEY_FILE: /certs/tls.key
    ports:
      - "443:443"
    volumes:
      - pennant-data:/data
      - /etc/letsencrypt/live/flags.example.com:/certs:ro
    restart: unless-stopped

volumes:
  pennant-data:
```

**Kubernetes Secret with certificate:**

```bash
kubectl create secret tls pennant-tls-cert \
  --cert=/path/to/tls.crt \
  --key=/path/to/tls.key \
  -n pennant
```

```yaml
# Mount in the deployment
volumes:
  - name: tls-certs
    secret:
      secretName: pennant-tls-cert
containers:
  - name: pennant
    env:
      - name: TLS_CERT_FILE
        value: /certs/tls.crt
      - name: TLS_KEY_FILE
        value: /certs/tls.key
    volumeMounts:
      - name: tls-certs
        mountPath: /certs
        readOnly: true
```

### Certificate rotation

When using manual certificates, restart the Pennant process after replacing the certificate files to pick up the new certificate. With Kubernetes, update the Secret and trigger a rolling restart:

```bash
kubectl rollout restart deployment/pennant -n pennant
```

## Mode 3: Plain HTTP (development only)

If neither `TLS_AUTO_DOMAIN` nor `TLS_CERT_FILE`+`TLS_KEY_FILE` is set, the server listens on plain HTTP at `:8080`.

```bash
PENNANT_JWT_SECRET=dev-secret SQLITE_PATH=./dev.db ./pennant-server
# Listens on http://localhost:8080
```

!!! danger "Do not use in production"
    Plain HTTP transmits JWT tokens, SDK keys, and all flag data in cleartext. Use TLS in any non-development environment.

## TLS behind a reverse proxy

If you terminate TLS at a reverse proxy (NGINX, Traefik, Cloudflare, AWS ALB) and route plain HTTP to Pennant internally, do not set any `TLS_*` environment variables. Pennant will listen on plain HTTP for internal traffic.

Make sure the proxy:
1. Enforces HTTPS externally (redirect HTTP → HTTPS).
2. Passes the `X-Forwarded-For` header (Pennant uses this for rate limiting).
3. Sets appropriate timeouts for SSE connections (at least 1 hour idle timeout — see [Kubernetes docs](kubernetes.md) for NGINX annotation example).

## Supported TLS versions

Pennant uses Go's standard `crypto/tls` package defaults:
- **Minimum:** TLS 1.2
- **Preferred:** TLS 1.3

TLS 1.0 and 1.1 are disabled by default (Go 1.18+).
