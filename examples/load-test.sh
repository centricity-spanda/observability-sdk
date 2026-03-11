#!/bin/bash
# ═══════════════════════════════════════════════════════════════════════════════
# Production-Grade Simulation — Load Test Script
# Tests all services with realistic scenarios: normal, error, slow, stress, PII
#
# Usage: ./load-test.sh [scenario]
#   Scenarios: all | normal | errors | slow | stress | pii
# ═══════════════════════════════════════════════════════════════════════════════

set -e

GO_URL="http://localhost:8088"
PY_URL="http://localhost:8089"
NET_URL="http://localhost:8091"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

SCENARIO="${1:-all}"

echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}  Observability PoC — Production Simulation   ${NC}"
echo -e "${CYAN}  Scenario: ${YELLOW}${SCENARIO}${NC}"
echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"

# ── Normal Traffic ──────────────────────────────────────────────────────────
run_normal() {
  echo -e "\n${GREEN}▶ [NORMAL TRAFFIC] Sending realistic requests...${NC}"

  for i in $(seq 1 20); do
    # Go service — payment
    curl -s -X POST "$GO_URL/api/payment" > /dev/null &
    # Python service — payment
    curl -s -X POST "$PY_URL/api/payment" > /dev/null &
    # .NET service — create order
    curl -s -X POST "$NET_URL/api/orders" \
      -H "Content-Type: application/json" \
      -d '{"symbol":"RELIANCE","qty":10,"price":2500.75}' > /dev/null &
    # .NET service — get order
    curl -s "$NET_URL/api/orders/ORD-20260223-12345" > /dev/null &
    # Health checks
    curl -s "$GO_URL/health" > /dev/null &
    curl -s "$PY_URL/health" > /dev/null &
    curl -s "$NET_URL/health" > /dev/null &
  done
  wait

  echo -e "${GREEN}  ✓ Sent 140 normal requests (20 rounds × 7 endpoints)${NC}"
}

# ── Error Spike ─────────────────────────────────────────────────────────────
run_errors() {
  echo -e "\n${RED}▶ [ERROR SPIKE] Triggering 500 errors across all services...${NC}"
  echo -e "${YELLOW}  This should trigger the 'High Error Rate' alert in Grafana${NC}"

  for i in $(seq 1 30); do
    curl -s -X POST "$GO_URL/api/error?count=$i" > /dev/null &
    curl -s -X POST "$PY_URL/api/error?count=$i" > /dev/null &
    curl -s -X POST "$NET_URL/api/error?count=$i" > /dev/null &
  done
  wait

  echo -e "${RED}  ✓ Sent 90 error requests (30 per service)${NC}"
  echo -e "${YELLOW}  → Check Grafana: Error Rate should spike above 5%${NC}"
}

# ── Slow Requests ───────────────────────────────────────────────────────────
run_slow() {
  echo -e "\n${YELLOW}▶ [SLOW REQUESTS] Sending requests with artificial delays...${NC}"
  echo -e "${YELLOW}  This should trigger the 'High P99 Latency' alert in Grafana${NC}"

  for i in $(seq 1 10); do
    # Go stress endpoint — 3 second delay
    curl -s -X POST "$GO_URL/api/stress?duration_ms=3000&memory_mb=0" > /dev/null &
    # Python stress — 3 second delay
    curl -s -X POST "$PY_URL/api/stress?duration_ms=3000&memory_mb=0" > /dev/null &
    # .NET slow endpoint — 3 second delay
    curl -s -X POST "$NET_URL/api/slow?delay_ms=3000" > /dev/null &
  done
  wait

  echo -e "${YELLOW}  ✓ Sent 30 slow requests (3s delay each)${NC}"
  echo -e "${YELLOW}  → Check Grafana: P99 latency should spike above 2s${NC}"
}

# ── Stress Test ─────────────────────────────────────────────────────────────
run_stress() {
  echo -e "\n${RED}▶ [STRESS TEST] CPU burn + memory allocation...${NC}"
  echo -e "${YELLOW}  This tests resource pressure and in-flight request alerts${NC}"

  for i in $(seq 1 5); do
    # 5-second CPU burn with 100MB memory
    curl -s -X POST "$GO_URL/api/stress?duration_ms=5000&memory_mb=100" > /dev/null &
    curl -s -X POST "$PY_URL/api/stress?duration_ms=5000&memory_mb=100" > /dev/null &
    curl -s -X POST "$NET_URL/api/stress?duration_ms=5000&memory_mb=100" > /dev/null &
  done
  wait

  echo -e "${RED}  ✓ Sent 15 stress requests (5s burn + 100MB each)${NC}"
}

# ── PII Redaction Test ──────────────────────────────────────────────────────
run_pii() {
  echo -e "\n${CYAN}▶ [PII TEST] Logging sensitive data to verify redaction...${NC}"

  for i in $(seq 1 5); do
    curl -s "$GO_URL/api/users" > /dev/null &
    curl -s "$PY_URL/api/users" > /dev/null &
    curl -s "$NET_URL/api/users" > /dev/null &
  done
  wait

  echo -e "${CYAN}  ✓ Sent 15 PII test requests${NC}"
  echo -e "${CYAN}  → Check Loki logs: PAN, Aadhaar, card numbers should be [MASKED]${NC}"
}

# ── Large Payload ───────────────────────────────────────────────────────────
run_large_payload() {
  echo -e "\n${YELLOW}▶ [LARGE PAYLOAD] Sending oversized payloads...${NC}"

  # Generate 2MB JSON payload
  PAYLOAD=$(python3 -c "import json; print(json.dumps({'data': 'x' * 2_000_000}))" 2>/dev/null || echo '{"data":"large_payload_test"}')

  curl -s -X POST "$GO_URL/api/large-payload" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" > /dev/null &
  curl -s -X POST "$PY_URL/api/large-payload" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" > /dev/null &
  wait

  echo -e "${YELLOW}  ✓ Sent 2 large payload requests (~2MB each)${NC}"
}

# ── Sustained Load (for proper metric generation) ──────────────────────────
run_sustained() {
  echo -e "\n${GREEN}▶ [SUSTAINED LOAD] Running 60 seconds of mixed traffic...${NC}"
  echo -e "${GREEN}  This generates enough data for meaningful dashboards...${NC}"

  for round in $(seq 1 60); do
    # Normal requests
    curl -s -X POST "$GO_URL/api/payment" > /dev/null &
    curl -s -X POST "$PY_URL/api/payment" > /dev/null &
    curl -s -X POST "$NET_URL/api/orders" \
      -H "Content-Type: application/json" \
      -d '{"symbol":"TCS","qty":5}' > /dev/null &

    # Occasional errors (10% error rate)
    if (( round % 10 == 0 )); then
      curl -s -X POST "$GO_URL/api/error" > /dev/null &
      curl -s -X POST "$PY_URL/api/error" > /dev/null &
      curl -s -X POST "$NET_URL/api/error" > /dev/null &
    fi

    # Occasional slow requests (5%)
    if (( round % 20 == 0 )); then
      curl -s -X POST "$NET_URL/api/slow?delay_ms=2000" > /dev/null &
    fi

    sleep 1
    printf "\r  Round %d/60" "$round"
  done
  wait
  echo -e "\n${GREEN}  ✓ Completed 60 seconds of sustained load${NC}"
}

# ── Execute Scenarios ───────────────────────────────────────────────────────
case "$SCENARIO" in
  normal)      run_normal ;;
  errors)      run_errors ;;
  slow)        run_slow ;;
  stress)      run_stress ;;
  pii)         run_pii ;;
  sustained)   run_sustained ;;
  all)
    run_normal
    sleep 2
    run_errors
    sleep 2
    run_slow
    sleep 2
    run_pii
    sleep 2
    run_large_payload
    sleep 2
    run_sustained
    ;;
  *)
    echo "Usage: $0 [all|normal|errors|slow|stress|pii|sustained]"
    exit 1
    ;;
esac

echo -e "\n${CYAN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  ✅ Simulation Complete!${NC}"
echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
echo -e ""
echo -e "  📊 Grafana:    ${YELLOW}http://localhost:3000${NC}  (admin / admin)"
echo -e "  📋 Loki:       ${YELLOW}http://localhost:3100${NC}"
echo -e "  📈 Mimir:      ${YELLOW}http://localhost:9009${NC}"
echo -e "  🔍 Tempo:      ${YELLOW}http://localhost:3200${NC}"
echo -e ""
echo -e "  Dashboards to check:"
echo -e "    → Service Health — RED Metrics"
echo -e "    → Platform Health — Meta-Monitoring"
echo -e ""
