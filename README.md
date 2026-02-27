# TelemetrySearch v2 — Microservices Architecture

## Architecture Overview

```
                    ┌──────────────────────────────────────────────┐
                    │              Browser / Client                │
                    └───────────────────┬──────────────────────────┘
                                        │ HTTP
                    ┌───────────────────▼──────────────────────────┐
                    │          API Gateway (Nginx :3000)           │
                    │   Rate limiting · Security headers · TLS     │
                    └──────┬──────────┬──────────────┬─────────────┘
                           │          │              │
              ┌────────────▼──┐  ┌────▼─────────┐  ┌▼──────────────┐
              │  Auth Service  │  │Search Service│  │Ingest Service │
              │   :8081       │  │   :8082      │  │   :8083       │
              │ JWT tokens     │  │ BM25 index   │  │ Upload + jobs │
              │ User mgmt      │  │ Read-only    │  │ Worker pool   │
              └───────────────┘  └──────────────┘  └───────────────┘
                                        │                    │
                                        └────────────────────┘
                                              Shared PVC
                                          (Parquet files)
```

## Services

| Service | Port | Responsibility | Auth Required |
|---------|------|----------------|---------------|
| `auth-service`   | 8081 | JWT login/refresh/validate, user management | No (public login) |
| `search-service` | 8082 | BM25 full-text search, stats | Yes — reader+ |
| `ingest-service` | 8083 | File upload, async job queue, file management | Yes — writer+ (upload/delete) |
| `gateway`        | 3000 | Nginx reverse proxy, rate limiting, routing | N/A |
| `frontend`       | 80   | React SPA with JWT auth flow | N/A |

## RBAC Roles

| Role     | Login | Search | View Stats | Upload Files | Delete Files | Trigger Reindex | Manage Users |
|----------|-------|--------|------------|--------------|--------------|-----------------|--------------|
| `reader` | ✅    | ✅     | ✅         | ❌           | ❌           | ❌              | ❌           |
| `writer` | ✅    | ✅     | ✅         | ✅           | ✅           | ✅              | ❌           |
| `admin`  | ✅    | ✅     | ✅         | ✅           | ✅           | ✅              | ✅           |

## JWT Token Flow

```
1. POST /api/v1/auth/login { username, password }
   → { access_token (15min), refresh_token (7days), expires_in }

2. Subsequent requests:
   Authorization: Bearer <access_token>

3. When access_token expires:
   POST /api/v1/auth/refresh { refresh_token }
   → { new access_token, new refresh_token }  ← token rotation (single-use refresh)

4. POST /api/v1/auth/logout { refresh_token }
   → Invalidates refresh token
```

## Quickstart (Docker Compose)

```bash
# 1. Create parquet data directory
mkdir -p parquet-data
cp path/to/your/*.parquet parquet-data/

# 2. Set secrets (optional — defaults are fine for dev)
export JWT_SECRET="your-long-random-secret"
export ADMIN_PASSWORD="your-admin-password"

# 3. Build and start all services
docker-compose up --build

# 4. Open browser: http://localhost:3000
# Default credentials: admin / admin123
```

## API Endpoints

### Auth Service (8081)
```
POST /api/v1/auth/login     - Login, receive JWT pair
POST /api/v1/auth/refresh   - Refresh access token (rotates refresh token)
POST /api/v1/auth/logout    - Invalidate refresh token
POST /api/v1/auth/validate  - Validate a token (used by other services)
POST /api/v1/admin/users    - Create user (admin only)
GET  /api/v1/admin/users    - List users (admin only)
GET  /health
```

### Search Service (8082)  [All routes require Bearer token]
```
GET  /api/v1/search?q=kafka&page=1&page_size=20  - BM25 search
GET  /api/v1/stats                               - Index statistics
POST /api/v1/reindex                             - Trigger reindex (writer+)
GET  /health
GET  /ready   - 503 until index is loaded (used by k8s readiness probe)
```

### Ingest Service (8083)  [All routes require Bearer token]
```
POST   /api/v1/upload          - Upload parquet file (writer+) → returns job_id
GET    /api/v1/files           - List uploaded files (reader+)
DELETE /api/v1/files/:name     - Delete a file (writer+)
GET    /api/v1/jobs            - List ingest jobs (reader+)
GET    /api/v1/jobs/:id        - Get job status (reader+)
GET    /health
GET    /ready
```

## Running Tests

```bash
# Auth service tests
cd auth-service && go test ./... -v -race

# Search service tests
cd search-service && go test ./... -v -race

# Ingest service tests
cd ingest-service && go test ./... -v -race

# Run all with coverage
for dir in auth-service search-service ingest-service; do
  echo "=== $dir ==="
  (cd $dir && go test ./... -cover -race)
done
```

## Kubernetes Deployment

```bash
# Apply all manifests in order
kubectl apply -f k8s/00-namespace-secrets.yaml
kubectl apply -f k8s/01-auth-service.yaml
kubectl apply -f k8s/02-search-service.yaml
kubectl apply -f k8s/03-ingest-service.yaml
kubectl apply -f k8s/04-frontend-gateway-ingress-hpa.yaml

# Update JWT secret before production:
kubectl create secret generic telemetry-jwt-secret \
  --from-literal=JWT_SECRET="$(openssl rand -base64 64)" \
  -n telemetry-search --dry-run=client -o yaml | kubectl apply -f -

# Watch rollout
kubectl rollout status deployment/search-service -n telemetry-search
```

## Phase 2b Roadmap

- [ ] WAL + snapshots for sub-5s index recovery on restart
- [ ] Index sharding (16 shards by namespace hash) for 50M+ docs
- [ ] Compressed posting lists (delta-encoded varints, 60% memory reduction)
- [ ] Read replicas with WAL broadcast (allows search-service HPA maxReplicas > 1)
- [ ] Prometheus metrics (`/metrics` endpoint on each service)
- [ ] OpenTelemetry distributed traces across services

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JWT_SECRET` | dev-secret | HMAC-SHA256 signing key — **change in production** |
| `ADMIN_PASSWORD` | admin123 | Default admin password — **change in production** |
| `WRITER_PASSWORD` | writer123 | Default writer password |
| `READER_PASSWORD` | reader123 | Default reader password |
| `DATA_DIR` | /app/parquet | Search service: directory to scan at startup |
| `UPLOAD_DIR` | /app/uploads | Ingest service: where to save uploaded files |
| `ALLOWED_ORIGIN` | http://localhost:3000 | CORS allowed origin |
| `PORT` | 8081/8082/8083 | HTTP listen port per service |
| `GIN_MODE` | release | `release` or `debug` |
