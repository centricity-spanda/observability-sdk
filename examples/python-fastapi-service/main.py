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
async def get_user_profile_nested(request: Request):
    """Example endpoint with nested PII data for testing redaction."""
    # Log with nested PII data - should be redacted recursively
    logger.info(
        "pii_redaction_test_event",
        trace_id=get_trace_id(),
        # 1. Sensitive Fields (Direct Match) -> [REDACTED]
        password="secret123",
        secret="my-secret-key",
        token="bearer-token-xyz",
        api_key="sk_test_123456",
        authorization="Bearer xyz",
        
        # 2. Regex Pattern Matching
        user_details={
            "email_address": "john.doe@example.com",
            "phone_number": "+919876543210", 
            "pan_card": "ABCDE1234F",
            "aadhaar_no": "8561 0272 7756",
            "credit_card": "4111-1111-1111-1111",
            "bank_account": "123456789012",
            "ifsc_code": "SBIN0123456",
            "passport_no": "A1234567",
            "ssn": "123-45-6789",
        },

        # 3. Skip Patterns (Should NOT be redacted)
        safe_data={
            "url": "https://example.com/user/12345",
            "file_url": "https://centricity-oms-vault.s3.ap-south-1.amazonaws.com/testgenerated/pdf/1771307830442-dedsas23456_20260217_055710.pdf",
            "file_path": "/var/log/app/12345.log",
            "windows_path": "C:\\Users\\John\\12345.txt",
            "request_uuid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
            "timestamp": "20230101120000",
            "version": "1.2.3",
            "order_id": "ORD-123456789",
        },

        # 4. Non-Sensitive Allowlist (Should NOT be redacted even if they look like PII)
        structural_data={
            "zipcode": "12345",     # Might look like part of SSN or phone
            "count": "100",
            "page": "1",
            "total": "500",
        }
    )
    
    return {"user_id": "USR-001", "name": "John Doe"}



if __name__ == "__main__":
    import uvicorn
    import os
    port = int(os.getenv("PORT", "8081"))
    uvicorn.run(app, host="0.0.0.0", port=port)
