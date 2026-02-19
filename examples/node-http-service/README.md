# Node HTTP Example Service

Example Node.js (Express) service using the **observability SDK** for logging, metrics, and tracing. Aligns with the Go and Python examples in this repo.

## Prerequisites

1. Build the SDK from the repo root:
   ```bash
   cd ../../js-sdk && npm run build && cd -
   ```
2. Node.js 18+

## Install and run

```bash
npm install
cp .env.example .env   # optional: edit for Kafka etc.
npm run build
npm start
```

Or run with ts-node (no build step):

```bash
npm install
npm run dev
```

Server listens on **http://localhost:8088** (or `PORT` from env).

## Endpoints

| Path           | Method | Description                    |
|----------------|--------|--------------------------------|
| `/health`      | GET    | Health check                   |
| `/api/payment` | POST   | Example payment (logs + span)  |
| `/api/users`   | GET    | Example with PII-style logs    |

## Environment

Same env vars as the Go/Python SDKs (see [js-sdk README](../../js-sdk/README.md)). For local testing without Kafka use:

- `ENVIRONMENT=development` — disables Kafka export; traces/metrics/logs still work (console).

## Middleware order

Tracing then metrics (same as Go/Python):

```ts
app.use(HTTPTracingMiddleware('example-service'));
app.use(HTTPMetricsMiddleware('example-service'));
```

## SDK dependency

This example depends on the local SDK via `file:../../js-sdk`. After changing the SDK, run `npm run build` in `js-sdk` and reinstall here if needed: `npm install`.
