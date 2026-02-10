"""Logger factory for Python services."""

import logging
import os
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional

import structlog
from structlog.types import Processor

from observability.logging.config import LogConfig, new_config
from observability.logging.kafka_handler import KafkaHandler
from observability.logging.pii_redactor import redact_log_event


_kafka_handler: Optional[KafkaHandler] = None
_file_handler: Optional[logging.FileHandler] = None


def _add_timestamp(logger: Any, method_name: str, event_dict: dict) -> dict:
    """Add ISO timestamp to log record."""
    event_dict["timestamp"] = datetime.now(timezone.utc).isoformat()
    return event_dict


def _pii_redact_processor(logger: Any, method_name: str, event_dict: dict) -> dict:
    """Apply PII redaction to log record."""
    return redact_log_event(event_dict)


def _kafka_processor(logger: Any, method_name: str, event_dict: dict) -> dict:
    """Send log record to Kafka."""
    global _kafka_handler
    if _kafka_handler:
        _kafka_handler.emit(event_dict)
    return event_dict


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
    global _kafka_handler, _file_handler
    
    config = new_config(service_name)
    
    # Initialize file handler if enabled
    if config.enable_file and _file_handler is None:
        _file_handler = _create_file_handler(config.log_file_path)
    
    # Initialize Kafka handler if not in development and enabled
    if not config.is_development and config.enable_kafka and _kafka_handler is None:
        _kafka_handler = KafkaHandler(config)
    
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
    
    # Add Kafka processor if enabled
    if config.enable_kafka and not config.is_development and _kafka_handler:
        processors.append(_kafka_processor)
    
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
