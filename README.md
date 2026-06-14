# Shield — UPI Fraud Detection Microservice

Shield is a fraud detection service built in Go for a PhonePe-style UPI payment system. It sits behind Kong Gateway and scores every payment/onboarding request — returning `ALLOW`, `BLOCK`, or `REVIEW` — before Kong routes to an upstream service.

---

## Architecture

```
curl / Postman
      │
      ▼
Kong OSS (Docker, port 8000)
      │
      ├── Plugin: shield-check (Lua)
      │       calls Shield at localhost:8082/check
      │       BLOCK  → 403 to caller
      │       REVIEW → 202 to caller
      │       ALLOW  → Kong routes to mock upstream
      │
      ├── /upi/pay      → mock-transaction (port 9001)
      └── /upi/register → mock-onboarding  (port 9002)

Docker Compose:
  ├── postgres   (port 5432)
  └── redis      (port 6379)

Go binaries (run directly on host):
  ├── shield            (port 8082)
  ├── mock-transaction  (port 9001)
  └── mock-onboarding   (port 9002)
```

---

## Fraud Rules

Rules run in order. Blacklist is a hard gate — if it fires, remaining rules are skipped.

| Rule | Signal | Score | Source |
|------|--------|-------|--------|
| Blacklist | VPA / IP / Device in blocklist | 100 → instant BLOCK | Postgres |
| Velocity | > 5 requests per user in 60s | +40 | Redis |
| Device | Unknown device for this user | +30 | Postgres |
| Amount | Transaction > ₹50,000 | +20 | Pure computation |

**Decision thresholds (demo values):**
- Score ≥ 40 → `BLOCK`
- Score ≥ 30 → `REVIEW`
- Score < 30 → `ALLOW`

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.22+ | https://go.dev/dl/ |
| Docker Desktop | 24+ | https://www.docker.com/products/docker-desktop/ |
| Docker Compose | v2 (bundled with Docker Desktop) | — |

> **Kong OSS via Homebrew is not available.** The `kong/kong` Homebrew tap was permanently disabled in 2023. Kong is run via Docker in this setup — no separate installation needed.

---

## Local Setup

### 1. Clone the repos

```bash
git clone https://github.com/krishnaMohan2501/shield.git
git clone https://github.com/krishnaMohan2501/mock-transaction.git
git clone https://github.com/krishnaMohan2501/mock-onboarding.git
git clone https://github.com/krishnaMohan2501/kong-shield.git
```

### 2. Start Postgres and Redis

```bash
cd shield
docker compose up -d
```

Expected:
```
✔ Container shield-postgres-1  Started
✔ Container shield-redis-1     Started
```

Verify Postgres is ready:
```bash
docker exec shield-postgres-1 psql -U shield -d shield_db -c "SELECT 1;"
```

### 3. Start Shield

```bash
cd shield
go run .
```

Expected output:
```
[SHIELD] Postgres schema ready
[SHIELD] Redis connected
[SHIELD] Starting on :8082
```

Verify:
```bash
curl http://localhost:8082/health
# {"status":"ok"}
```

### 4. Start mock upstream services (two new terminals)

**Terminal 2 — mock-transaction:**
```bash
cd mock-transaction
go run .
# [MOCK-TXN] Starting on :9001
```

**Terminal 3 — mock-onboarding:**
```bash
cd mock-onboarding
go run .
# [MOCK-ONB] Starting on :9002
```

### 5. Start Kong OSS (Docker)

```bash
docker run -d --name kong \
  -p 8000:8000 -p 8001:8001 \
  --add-host=host.docker.internal:host-gateway \
  -e KONG_DATABASE=off \
  -e KONG_DECLARATIVE_CONFIG=/etc/kong/kong.yml \
  -e KONG_PLUGINS=bundled,shield-check \
  -v $(pwd)/../kong-shield/kong.yml:/etc/kong/kong.yml \
  -v $(pwd)/../kong-shield/plugins/shield-check:/usr/local/share/lua/5.1/kong/plugins/shield-check \
  kong:3.9
```

Wait ~8 seconds, then verify Kong is up:
```bash
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-user-id: test" -H "x-device-id: d1" -H "x-amount: 100" -H "x-receiver-vpa: ok@upi"
# Returns JSON (202 REVIEW or 200 ALLOW) — any JSON response means Kong is routing correctly
# A 404 means you used GET instead of POST (routes are POST-only)
# A "000" / connection refused means Kong is not running
```

> **Why `--add-host=host.docker.internal:host-gateway`?**
> Kong runs inside Docker and needs to reach Shield and mock services running on your Mac. This flag maps `host.docker.internal` to the host machine's gateway IP — the standard way to reach host services from Docker containers on macOS.

---

## Testing

All requests go through Kong on port `8000`. Fraud signals are passed as HTTP headers.

### ALLOW — known device, normal amount

```bash
# First request — new device triggers REVIEW (score 30)
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: REQ-001" \
  -H "x-user-id: user_123" \
  -H "x-device-id: device_abc" \
  -H "x-amount: 500" \
  -H "x-receiver-vpa: merchant@okaxis"
# {"message":"Transaction flagged for review"}  ← REVIEW (202)

# Second request — device now registered, ALLOW
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: REQ-002" \
  -H "x-user-id: user_123" \
  -H "x-device-id: device_abc" \
  -H "x-amount: 500" \
  -H "x-receiver-vpa: merchant@okaxis"
# {"status":"payment_accepted","transactionId":"TXN-MOCK-001"}  ← ALLOW (200)
```

### BLOCK — blacklisted VPA

```bash
# Add a VPA to the blacklist
docker exec shield-postgres-1 psql -U shield -d shield_db \
  -c "INSERT INTO blacklist (type, value, reason) VALUES ('VPA', 'fraud@scam', 'known fraudster');"

curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: REQ-BL" \
  -H "x-user-id: user_999" \
  -H "x-device-id: device_999" \
  -H "x-amount: 100" \
  -H "x-receiver-vpa: fraud@scam"
# {"error":"Transaction blocked","reason":"blacklisted_entity"}  ← BLOCK (403)
```

### BLOCK — velocity (> 5 requests/minute)

```bash
# Step 1: register device with first request (expect REVIEW — new device)
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: WARM" \
  -H "x-user-id: spammer" \
  -H "x-device-id: d1" \
  -H "x-amount: 100" \
  -H "x-receiver-vpa: ok@upi"

# Step 2: wait for velocity window to reset, then flood (6 rapid requests)
sleep 61
for i in {1..6}; do
  curl -s -o /dev/null -w "Request $i: %{http_code}\n" \
    -X POST http://localhost:8000/upi/pay \
    -H "x-request-id: V$i" \
    -H "x-user-id: spammer" \
    -H "x-device-id: d1" \
    -H "x-amount: 100" \
    -H "x-receiver-vpa: ok@upi"
done
# Request 1: 200
# Request 2: 200
# Request 3: 200
# Request 4: 200
# Request 5: 200
# Request 6: 403  ← velocity BLOCK
```

### Verify audit log

```bash
docker exec shield-postgres-1 psql -U shield -d shield_db \
  -c "SELECT request_id, decision, risk_score, triggered_rules FROM fraud_audit_log ORDER BY created_at DESC LIMIT 10;"
```

---

## Configuration

All thresholds are in `shield/.env` — no recompile needed to change them.

```bash
VELOCITY_MAX_TXN_PER_MINUTE=5      # requests per user per 60s before velocity fires
AMOUNT_HIGH_VALUE_THRESHOLD=50000  # amount in INR above which high_amount fires
RISK_BLOCK_THRESHOLD=40            # total score >= this → BLOCK
RISK_REVIEW_THRESHOLD=30           # total score >= this → REVIEW
```

---

## Stopping Everything

```bash
# Stop Kong
docker rm -f kong

# Stop Postgres + Redis
cd shield && docker compose down

# Stop Go processes
pkill -f "go run ."
```

---

## Related Repos

| Repo | Description |
|------|-------------|
| [shield](https://github.com/krishnaMohan2501/shield) | This service — fraud scoring engine |
| [mock-transaction](https://github.com/krishnaMohan2501/mock-transaction) | Mock UPI payment upstream (port 9001) |
| [mock-onboarding](https://github.com/krishnaMohan2501/mock-onboarding) | Mock UPI registration upstream (port 9002) |
| [kong-shield](https://github.com/krishnaMohan2501/kong-shield) | Kong declarative config + shield-check Lua plugin |
