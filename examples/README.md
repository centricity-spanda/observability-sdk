# Centricity Observability PoC

Full-stack observability platform with logs, metrics, traces, dashboards, and alerting.

## Quick Start

### Phase 1 — No Kafka (Recommended for ≤ 20 services)

```bash
cd examples
docker-compose -f docker-compose.no-kafka.yml up -d --build
```

| Service | URL |
|---|---|
| **Grafana** (dashboards + alerts) | [http://localhost:3000](http://localhost:3000) (admin / admin) |
| Go Service | http://localhost:8088 |
| Python Service | http://localhost:8089 |
| .NET Service | http://localhost:8091 |
| Loki (logs) | http://localhost:3100 |
| Mimir (metrics) | http://localhost:9009 |
| Tempo (traces) | http://localhost:3200 |

### Phase 2 — With Kafka (For 20+ services)

```bash
cd examples
docker-compose -f docker-compose.kafka.yml up -d --build
```

Adds: Kafka (http://localhost:9092), Kafka UI (http://localhost:8080), Vector Aggregator, OTel Gateway.

---

## Load Testing (Production Simulation)

```bash
# Run ALL scenarios (normal → errors → slow → PII → stress → sustained)
bash load-test.sh all

# Or run individual scenarios:
bash load-test.sh normal      # 140 normal requests
bash load-test.sh errors      # 90 error requests (triggers High Error Rate alert)
bash load-test.sh slow        # 30 slow requests with 3s delay (triggers P99 alert)
bash load-test.sh stress      # 15 CPU/memory burn requests
bash load-test.sh pii         # 15 PII-sensitive data requests (check Loki for masking)
bash load-test.sh sustained   # 60 seconds of mixed traffic
```

---

## Service Endpoints

All services expose production-grade simulation endpoints:

| Endpoint | Method | Purpose |
|---|---|---|
| `/health` | GET | Health check |
| `/api/payment` | POST | Normal payment processing (Go/Python) |
| `/api/orders` | POST | Create order (Go/Python/.NET) |
| `/api/error?count=N` | POST | Trigger HTTP 500 errors (all services) |
| `/api/stress?duration_ms=N&memory_mb=M` | POST | CPU burn + memory allocation (all services) |
| `/api/slow?delay_ms=N` | POST | Artificially slow endpoint (.NET) |
| `/api/users` | GET | PII data logging test (all services) |
| `/api/large-payload` | POST | Large request body test (Go/Python) |

---

## Grafana Dashboards

| Dashboard | Description |
|---|---|
| **Service Health — RED Metrics** | Request rate, error rate %, P50/P95/P99 latency, in-flight requests, log volume, error log panel, service comparison table |
| **Platform Health — Meta-Monitoring** | Loki ingestion rate + stream count, Mimir active series, OTel dropped spans/metrics + queue size, Tempo spans received, Vector throughput + errors |

---

## Alert Rules

### Service Health (5 rules)
| Alert | Threshold | Severity |
|---|---|---|
| High Error Rate | > 5% for 1 min | Critical |
| High P99 Latency | > 2s for 2 min | Warning |
| High In-Flight Requests | > 50 for 1 min | Warning |
| Large Request Payload | P99 > 1MB | Critical |
| Kafka Producer Failures | Any errors | Critical |

### Platform Health — Meta-Monitoring (4 rules)
| Alert | Threshold | Severity |
|---|---|---|
| Vector Agent Errors | > 0 for 5 min | Critical |
| OTel Collector Drops | > 0 for 5 min | Critical |
| Loki Streams Nearing Limit | > 8,000 for 10 min | Warning |
| Mimir Series Nearing Limit | > 800,000 for 10 min | Warning |

---

## Architecture

```
Phase 1 (No Kafka):
  Apps → Vector Agent → Loki (direct)
  Apps → OTel Agent  → Tempo/Mimir (direct)

Phase 2 (With Kafka):
  Apps → Vector Agent → Kafka → Vector Aggregator → Loki
  Apps → OTel Agent  → Kafka → OTel Gateway      → Tempo/Mimir
```

See [Centricity_Observability_Architecture_v2.md](../docs/Centricity_Observability_Architecture_v2.md) for the full architecture document with HLD, LLD, FR/NFR, and phase plan.

---

## Rate Limiting & Cardinality Protection

| Layer | Protection |
|---|---|
| **Loki** | 10K max streams, 3MB/s per stream, 256KB max line |
| **Mimir** | 1M max series, 30 labels max, 50K series per query |
| **Vector** | 1,000 events/sec per service (throttle transform) |
| **OTel Collector** | 512MB memory limit, 1,000 batch size |

## Stop

```bash
docker-compose -f docker-compose.no-kafka.yml down -v   # Phase 1
docker-compose -f docker-compose.kafka.yml down -v       # Phase 2
```
