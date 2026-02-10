#!/bin/bash

# ELK Stack + Kafka Test Script
# This script starts the infrastructure and tests the observability SDK

set -e

echo "========================================="
echo "  Platform Observability SDK Test"
echo "  ELK Stack + Kafka"
echo "========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running${NC}"
    exit 1
fi

echo -e "\n${YELLOW}Step 1: Starting infrastructure...${NC}"
docker-compose up -d

echo -e "\n${YELLOW}Step 2: Waiting for services to be healthy...${NC}"

# Wait for Elasticsearch
echo -n "  Waiting for Elasticsearch..."
until curl -s http://localhost:9200/_cluster/health > /dev/null 2>&1; do
    echo -n "."
    sleep 2
done
echo -e " ${GREEN}Ready!${NC}"

# Wait for Kibana
echo -n "  Waiting for Kibana..."
until curl -s http://localhost:5601/api/status > /dev/null 2>&1; do
    echo -n "."
    sleep 2
done
echo -e " ${GREEN}Ready!${NC}"

# Wait for Kafka
echo -n "  Waiting for Kafka..."
until docker exec kafka kafka-topics --bootstrap-server localhost:9092 --list > /dev/null 2>&1; do
    echo -n "."
    sleep 2
done
echo -e " ${GREEN}Ready!${NC}"

echo -e "\n${GREEN}========================================="
echo "  Infrastructure Ready!"
echo "=========================================${NC}"
echo ""
echo "Available Services:"
echo "  - Kibana:         http://localhost:5601"
echo "  - Elasticsearch:  http://localhost:9200"
echo "  - Kafka UI:       http://localhost:8080"
echo "  - Kafka:          localhost:9092"
echo ""
echo -e "${YELLOW}Next Steps:${NC}"
echo "  1. Build and run the Go example service:"
echo "     cd go-http-service"
echo "     export KAFKA_BROKERS=localhost:9092"
echo "     export ENVIRONMENT=production"
echo "     go run main.go"
echo ""
echo "  2. Generate some logs by calling the API:"
echo "     curl http://localhost:8088/api/users"
echo "     curl http://localhost:8088/health"
echo ""
echo "  3. View logs in Kibana:"
echo "     - Open http://localhost:5601"
echo "     - Go to Management > Stack Management > Index Patterns"
echo "     - Create index pattern: logs-*"
echo "     - Go to Analytics > Discover"
echo ""
echo "  4. Check Kafka messages in Kafka UI:"
echo "     - Open http://localhost:8080"
echo "     - Browse topics: logs.application, metrics.application, traces.application"
echo ""
echo -e "${YELLOW}To stop:${NC} docker-compose down"
