"""Logger factory for Python services."""

import logging
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Optional, Tuple

import structlog
from structlog.types import Processor

from observability.logging.config import LogConfig, new_config
from observability.logging.pii_redactor import redact_log_event


_file_handler: Optional[logging.FileHandler] = None


def _add_timestamp(logger: Any, method_name: str, event_dict: dict) -> dict:
    """Add ISO timestamp to log record."""
    event_dict["timestamp"] = datetime.now(timezone.utc).isoformat()
    return event_dict


def _pii_redact_processor(logger: Any, method_name: str, event_dict: dict) -> dict:
    """Apply PII redaction to log record."""
    return redact_log_event(event_dict)


def _file_processor(logger: Any, method_name: str, event_dict: dict) -> dict:
    """Write log record to file."""
    global _file_handler
    if _file_handler:
        import json
        log_line = json.dumps(event_dict, default=str)
        _file_handler.stream.write(log_line + "\n")
        _file_handler.stream.flush()
    return event_dict


def new_logger(service_name: str) -> structlog.BoundLogger:
    """Create a new structured logger with configurable outputs.
    
    Args:
        service_name: The name of the service for log identification.
    
    Returns:
        A configured structlog logger.
    """
    global _file_handler
    
    config = new_config(service_name)
    
    # Initialize file handler if enabled
    if config.enable_file and _file_handler is None:
        _file_handler = _create_file_handler(config.log_file_path)

    def _format_log_schema(logger: Any, method_name: str, event_dict: Dict[str, Any]) -> Dict[str, Any]:
        """Shape log record into the unified envelope schema.

        Input:  flat structlog event dict (already PII-redacted).
        Output: envelope with timestamp, severity, service block, attributes, error.
        """
        # Pull out basic fields
        timestamp = event_dict.pop("timestamp", datetime.now(timezone.utc).isoformat())
        severity = str(event_dict.pop("level", method_name.upper())).upper()
        message = str(event_dict.pop("event", ""))

        severity_num = _severity_to_number(severity)

        # Build service block from config
        service_block: Dict[str, Any] = {
            "service.name": config.service_name,
            "service.version": config.service_version,
            "service.namespace": config.service_namespace,
            "deployment.environment": config.environment,
        }
        if config.host_name:
            service_block["host.name"] = config.host_name
        if config.k8s_pod_name:
            service_block["k8s.pod.name"] = config.k8s_pod_name
        if config.k8s_namespace_name:
            service_block["k8s.namespace.name"] = config.k8s_namespace_name
        if config.k8s_node_name:
            service_block["k8s.node.name"] = config.k8s_node_name

        # Extract error details if present
        error_block = {}
        if "exception" in event_dict:
            error_block = {"exception": event_dict.pop("exception")}
        elif "error" in event_dict and isinstance(event_dict["error"], dict):
            error_block = event_dict.pop("error")

        # Remaining keys are treated as attributes
        attributes = dict(event_dict)

        return {
            "timestamp": timestamp,
            "severity": severity,
            "severity_num": severity_num,
            "message": message,
            "service": service_block,
            "attributes": attributes,
            "error": error_block,
        }
    
    # Build processor chain
    processors: list[Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        _add_timestamp,
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
    ]
    
    # Add PII redaction if enabled (before any output)
    if config.enable_pii_redaction:
        processors.append(_pii_redact_processor)
    
    # Shape into unified log schema envelope
    processors.append(_format_log_schema)

    # Add file processor if enabled
    if config.enable_file and _file_handler:
        processors.append(_file_processor)
    
    # Add JSON renderer
    processors.append(structlog.processors.JSONRenderer())
    
    # Determine output
    output_file = sys.stdout if config.enable_console else None
    
    # Configure structlog
    structlog.configure(
        processors=processors,
        wrapper_class=structlog.make_filtering_bound_logger(
            _log_level_to_int(config.log_level)
        ),
        context_class=dict,
        logger_factory=structlog.PrintLoggerFactory(file=output_file or sys.stdout),
        cache_logger_on_first_use=True,
    )
    
    # Create logger with default context
    logger = structlog.get_logger().bind(
        service=config.service_name,
        environment=config.environment,
        version=config.service_version,
    )
    
    return logger


def _log_level_to_int(level: str) -> int:
    """Convert log level string to integer."""
    levels = {
        "debug": 10,
        "info": 20,
        "warn": 30,
        "warning": 30,
        "error": 40,
        "critical": 50,
    }
    return levels.get(level.lower(), 20)


def _severity_to_number(severity: str) -> int:
    """Map severity text to OpenTelemetry-style numeric severity."""
    mapping = {
        "TRACE": 1,
        "DEBUG": 5,
        "INFO": 9,
        "WARN": 13,
        "WARNING": 13,
        "ERROR": 17,
        "FATAL": 21,
        "CRITICAL": 21,
    }
    return mapping.get(severity.upper(), 9)


def _create_file_handler(path: str) -> Optional[logging.FileHandler]:
    """Create a file handler for logging."""
    try:
        # Create directory if needed
        log_path = Path(path)
        log_path.parent.mkdir(parents=True, exist_ok=True)
        
        handler = logging.FileHandler(path, mode='a', encoding='utf-8')
        return handler
    except Exception as e:
        sys.stderr.write(f"Warning: Failed to create log file: {e}\n")
        return None
