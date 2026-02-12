"""Example FastAPI service with observability."""

import asyncio
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from dotenv import load_dotenv

load_dotenv()

from observability import new_logger, start_metrics_pusher, new_tracer, get_trace_id
from observability.metrics.middleware import HTTPMetricsMiddleware
from observability.tracing.middleware import HTTPTracingMiddleware


# Initialize observability
logger = new_logger("example-service-python")
tracer = new_tracer("example-service-python")


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Startup and shutdown events."""
    # Startup
    start_metrics_pusher("example-service-python")
    logger.info("service starting")
    yield
    # Shutdown
    logger.info("service shutting down")


app = FastAPI(title="Example Service", lifespan=lifespan)

# Add middleware (order matters - tracing first)
app.add_middleware(HTTPTracingMiddleware, service_name="example-service-python")
app.add_middleware(HTTPMetricsMiddleware, service_name="example-service-python")


@app.get("/health")
async def health():
    """Health check endpoint."""
    return {"status": "healthy"}


@app.post("/api/payment")
async def process_payment(request: Request):
    """Example payment endpoint with observability."""
    
    # Log with trace correlation
    logger.info(
        "processing payment",
        trace_id=get_trace_id(),
        payment_id="PAY-12345",
        amount=1500.50,
    )
    
    # Simulate work
    with tracer.start_as_current_span("validate_payment"):
        await asyncio.sleep(0.05)  # Simulate validation
    
    with tracer.start_as_current_span("process_transaction"):
        await asyncio.sleep(0.1)  # Simulate processing
    
    logger.info("payment completed", payment_id="PAY-12345")
    
    return {"status": "completed", "payment_id": "PAY-12345"}


@app.get("/api/users")
async def get_user_profile(request: Request):
    """Example endpoint with PII data for testing redaction."""
    # Log with PII data - should be redacted
    logger.info(
        "user profile accessed",
        trace_id=get_trace_id(),
        user_id="USR-001",
        email="john.doe@example.com",      # Should be redacted
        pan="ABCDE1234F",                  # Should be redacted
        aadhaar="1234 5678 9012",          # Should be redacted
        phone="+91 9876543210",            # Should be redacted
        card_number="4111-1111-1111-1111", # Should be redacted
    )
    
    return {"user_id": "USR-001", "name": "John Doe"}


if __name__ == "__main__":
    import uvicorn
    import os
    port = int(os.getenv("PORT", "8081"))
    uvicorn.run(app, host="0.0.0.0", port=port)
