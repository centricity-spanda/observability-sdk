"""Logging configuration."""

import os
from dataclasses import dataclass, field
from typing import List


@dataclass
class LogConfig:
    """Configuration for logging."""
    
    service_name: str
    environment: str = field(default_factory=lambda: os.getenv("ENVIRONMENT", "production"))
    service_version: str = field(default_factory=lambda: os.getenv("SERVICE_VERSION", "unknown"))
    kafka_brokers: List[str] = field(default_factory=list)
    log_topic: str = field(default_factory=lambda: os.getenv("KAFKA_LOG_TOPIC", "logs.application"))
    log_level: str = field(default_factory=lambda: os.getenv("LOG_LEVEL", "info"))
    log_type: str = field(default_factory=lambda: os.getenv("LOG_TYPE", "standard"))
    buffer_size: int = 4096
    
    # Export controls
    enable_kafka: bool = field(default_factory=lambda: _get_env_bool("LOG_KAFKA_ENABLED", True))
    enable_file: bool = field(default_factory=lambda: _get_env_bool("LOG_FILE_ENABLED", False))
    log_file_path: str = field(default_factory=lambda: os.getenv("LOG_FILE_PATH", "./logs/app.log"))
    enable_console: bool = field(default_factory=lambda: _get_env_bool("LOG_CONSOLE_ENABLED", True))
    
    # PII redaction
    enable_pii_redaction: bool = field(default_factory=lambda: _get_env_bool("LOG_PII_REDACTION_ENABLED", True))
    
    def __post_init__(self):
        """Parse Kafka brokers from environment if not provided."""
        if not self.kafka_brokers:
            brokers = os.getenv("KAFKA_BROKERS", "")
            if brokers:
                self.kafka_brokers = [b.strip() for b in brokers.split(",")]
    
    @property
    def is_development(self) -> bool:
        """Check if running in development mode."""
        return self.environment == "development"
    
    @property
    def is_audit_log(self) -> bool:
        """Check if configured for audit logging."""
        return self.log_type == "audit"


def new_config(service_name: str) -> LogConfig:
    """Create a new LogConfig from environment variables."""
    return LogConfig(service_name=service_name)


def _get_env_bool(key: str, default: bool = True) -> bool:
    """Get boolean value from environment variable."""
    value = os.getenv(key, "").lower()
    if not value:
        return default
    return value in ("true", "1", "yes")
