"""HTTP tracing middleware for FastAPI and Flask."""

from typing import Callable

from opentelemetry import trace
from opentelemetry.trace import SpanKind, Status, StatusCode


class HTTPTracingMiddleware:
    """ASGI middleware for distributed tracing (FastAPI/Starlette)."""
    
    def __init__(self, app, service_name: str):
        self.app = app
        self.service_name = service_name
        self.tracer = trace.get_tracer(service_name)
    
    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        
        method = scope.get("method", "GET")
        path = scope.get("path", "/")
        span_name = f"{method} {path}"
        
        # Extract headers for context propagation (if needed)
        headers = dict(scope.get("headers", []))
        
        status_code = 500
        
        async def send_wrapper(message):
            nonlocal status_code
            if message["type"] == "http.response.start":
                status_code = message["status"]
            await send(message)
        
        with self.tracer.start_as_current_span(
            span_name,
            kind=SpanKind.SERVER,
            attributes={
                "http.method": method,
                "http.route": path,
                "http.scheme": scope.get("scheme", "http"),
                "http.host": headers.get(b"host", b"").decode() if b"host" in headers else "",
            },
        ) as span:
            try:
                await self.app(scope, receive, send_wrapper)
                
                # Set status code attribute
                span.set_attribute("http.status_code", status_code)
                
                # Mark as error if 4xx or 5xx
                if status_code >= 400:
                    span.set_status(Status(StatusCode.ERROR))
            except Exception as e:
                span.set_status(Status(StatusCode.ERROR, str(e)))
                span.record_exception(e)
                raise


def flask_tracing_middleware(service_name: str) -> Callable:
    """Create Flask before/after request handlers for tracing.
    
    Usage:
        from flask import Flask, g
        app = Flask(__name__)
        before, after, teardown = flask_tracing_middleware("my-service")
        app.before_request(before)
        app.after_request(after)
        app.teardown_request(teardown)
    """
    from flask import g, request
    
    tracer = trace.get_tracer(service_name)
    
    def before_request():
        span_name = f"{request.method} {request.path}"
        span = tracer.start_span(
            span_name,
            kind=SpanKind.SERVER,
            attributes={
                "http.method": request.method,
                "http.route": request.path,
                "http.scheme": request.scheme,
                "http.host": request.host,
            },
        )
        g._trace_span = span
        g._trace_token = trace.use_span(span, end_on_exit=False)
        g._trace_token.__enter__()
    
    def after_request(response):
        span = getattr(g, "_trace_span", None)
        if span:
            span.set_attribute("http.status_code", response.status_code)
            if response.status_code >= 400:
                span.set_status(Status(StatusCode.ERROR))
        return response
    
    def teardown_request(exception):
        token = getattr(g, "_trace_token", None)
        span = getattr(g, "_trace_span", None)
        if token:
            token.__exit__(None, None, None)
        if span:
            if exception:
                span.set_status(Status(StatusCode.ERROR, str(exception)))
                span.record_exception(exception)
            span.end()
    
    return before_request, after_request, teardown_request
