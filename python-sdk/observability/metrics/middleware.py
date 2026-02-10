"""HTTP middleware for metrics collection."""

import time
from typing import Callable

from observability.metrics.registry import (
    http_requests_total,
    http_request_duration_seconds,
    http_requests_in_flight,
)


class HTTPMetricsMiddleware:
    """ASGI middleware for recording HTTP metrics (FastAPI/Starlette)."""
    
    def __init__(self, app, service_name: str):
        self.app = app
        self.service_name = service_name
    
    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        
        # Track in-flight requests
        http_requests_in_flight.labels(service=self.service_name).inc()
        
        start_time = time.perf_counter()
        status_code = 500  # Default in case of error
        
        async def send_wrapper(message):
            nonlocal status_code
            if message["type"] == "http.response.start":
                status_code = message["status"]
            await send(message)
        
        try:
            await self.app(scope, receive, send_wrapper)
        finally:
            # Record metrics
            duration = time.perf_counter() - start_time
            method = scope.get("method", "GET")
            path = scope.get("path", "/")
            
            http_requests_total.labels(
                service=self.service_name,
                method=method,
                path=self._normalize_path(path),
                status=str(status_code),
            ).inc()
            
            http_request_duration_seconds.labels(
                service=self.service_name,
                method=method,
                path=self._normalize_path(path),
            ).observe(duration)
            
            http_requests_in_flight.labels(service=self.service_name).dec()
    
    @staticmethod
    def _normalize_path(path: str) -> str:
        """Normalize path to reduce cardinality."""
        if len(path) > 50:
            return path[:50] + "..."
        return path


def flask_metrics_middleware(service_name: str) -> Callable:
    """Create Flask before/after request handlers for metrics.
    
    Usage:
        from flask import Flask, g
        app = Flask(__name__)
        before, after = flask_metrics_middleware("my-service")
        app.before_request(before)
        app.after_request(after)
    """
    from flask import g, request
    
    def before_request():
        g._start_time = time.perf_counter()
        http_requests_in_flight.labels(service=service_name).inc()
    
    def after_request(response):
        duration = time.perf_counter() - getattr(g, "_start_time", time.perf_counter())
        path = request.path[:50] if len(request.path) > 50 else request.path
        
        http_requests_total.labels(
            service=service_name,
            method=request.method,
            path=path,
            status=str(response.status_code),
        ).inc()
        
        http_request_duration_seconds.labels(
            service=service_name,
            method=request.method,
            path=path,
        ).observe(duration)
        
        http_requests_in_flight.labels(service=service_name).dec()
        return response
    
    return before_request, after_request
