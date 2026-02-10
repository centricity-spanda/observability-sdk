@echo off
REM ELK Stack + Vector + OTEL Collector Test Script (Windows)

echo =========================================
echo   Platform Observability SDK Test
echo   Kafka + Vector + OTEL Collector + ELK
echo =========================================

REM Check if Docker is running
docker info >nul 2>&1
if errorlevel 1 (
    echo Error: Docker is not running
    exit /b 1
)

echo.
echo Step 1: Starting infrastructure...
docker-compose up -d

echo.
echo Step 2: Waiting for services to be healthy...
echo   (This may take 1-2 minutes)

REM Wait for Elasticsearch
echo   Waiting for Elasticsearch...
:wait_es
curl -s http://localhost:9200/_cluster/health >nul 2>&1
if errorlevel 1 (
    timeout /t 2 /nobreak >nul
    goto wait_es
)
echo   Elasticsearch Ready!

echo.
echo =========================================
echo   Infrastructure Ready!
echo =========================================
echo.
echo Pipeline Architecture:
echo.
echo   App --^> Kafka --^> Vector --^> Elasticsearch (logs)
echo                  ^|
echo                  +--^> OTEL Collector --^> Elasticsearch (traces/metrics)
echo.
echo.
echo Available Services:
echo   - Kibana:           http://localhost:5601
echo   - Elasticsearch:    http://localhost:9200
echo   - Kafka UI:         http://localhost:8080
echo   - Vector API:       http://localhost:8686
echo   - OTEL Health:      http://localhost:13133
echo   - OTEL Metrics:     http://localhost:8888/metrics
echo   - Kafka:            localhost:9092
echo.
echo Next Steps:
echo   1. Run the Go example service:
echo      cd go-http-service
echo      set KAFKA_BROKERS=localhost:9092
echo      set ENVIRONMENT=production
echo      go run main.go
echo.
echo   2. Generate logs with PII (will be redacted):
echo      curl http://localhost:8088/api/users
echo      curl http://localhost:8088/api/payment
echo.
echo   3. View in Kibana:
echo      - Open http://localhost:5601
echo      - Create index patterns: logs-*, traces-*, metrics-*
echo      - Go to Discover
echo.
echo To stop: docker-compose down -v
