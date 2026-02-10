"""PII redaction for log messages."""

import re
from typing import Any, Dict, List


class PIIRedactor:
    """Redacts PII from log messages using regex patterns."""
    
    # Pre-compiled regex patterns for FinTech PII
    PATTERNS = [
        # PAN Card (India): ABCDE1234F
        (re.compile(r'\b[A-Z]{5}[0-9]{4}[A-Z]\b'), '[PAN_REDACTED]'),
        # Aadhaar (India): 1234 5678 9012 or 123456789012
        (re.compile(r'\b\d{4}\s?\d{4}\s?\d{4}\b'), '[AADHAAR_REDACTED]'),
        # Credit/Debit Card Numbers: 16 digits with optional spaces/dashes
        (re.compile(r'\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b'), '[CARD_REDACTED]'),
        # Email addresses
        (re.compile(r'\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b'), '[EMAIL_REDACTED]'),
        # Phone numbers (India): +91 or 0 followed by 10 digits
        (re.compile(r'\b(?:\+91[\s-]?|0)?[6-9]\d{9}\b'), '[PHONE_REDACTED]'),
        # Bank Account Numbers: 9-18 digits
        (re.compile(r'\b\d{9,18}\b'), '[ACCOUNT_REDACTED]'),
        # IFSC Code: 4 letters + 0 + 6 alphanumeric
        (re.compile(r'\b[A-Z]{4}0[A-Z0-9]{6}\b'), '[IFSC_REDACTED]'),
        # Passport Number: Letter followed by 7 digits
        (re.compile(r'\b[A-Z][0-9]{7}\b'), '[PASSPORT_REDACTED]'),
        # SSN (US): XXX-XX-XXXX
        (re.compile(r'\b\d{3}-\d{2}-\d{4}\b'), '[SSN_REDACTED]'),
    ]
    
    # Field names that should be completely masked
    SENSITIVE_FIELDS = frozenset({
        'password', 'secret', 'token', 'api_key', 'apikey',
        'authorization', 'credit_card', 'card_number', 'cvv',
        'pin', 'ssn', 'aadhaar', 'pan', 'account_number',
        'routing_number', 'private_key',
    })
    
    def __init__(self):
        """Initialize the redactor."""
        pass
    
    def redact(self, value: str) -> str:
        """Apply all PII redaction patterns to a string."""
        result = value
        for pattern, replacement in self.PATTERNS:
            result = pattern.sub(replacement, result)
        return result
    
    def is_sensitive_field(self, field_name: str) -> bool:
        """Check if a field name is sensitive."""
        return field_name.lower() in self.SENSITIVE_FIELDS
    
    def redact_dict(self, data: Dict[str, Any]) -> Dict[str, Any]:
        """Recursively redact PII from a dictionary."""
        result = {}
        for key, value in data.items():
            result[key] = self.redact_value(key, value)
        return result
    
    def redact_value(self, key: str, value: Any) -> Any:
        """Redact a single value based on field name and content."""
        # Completely mask sensitive fields
        if self.is_sensitive_field(key):
            return '[REDACTED]'
        
        if isinstance(value, str):
            return self.redact(value)
        elif isinstance(value, dict):
            return self.redact_dict(value)
        elif isinstance(value, list):
            return [self.redact_value('', item) for item in value]
        
        return value


# Global redactor instance
_redactor = PIIRedactor()


def redact_log_event(event_dict: Dict[str, Any]) -> Dict[str, Any]:
    """Redact PII from a structlog event dictionary."""
    return _redactor.redact_dict(event_dict)


def get_redactor() -> PIIRedactor:
    """Get the global PII redactor instance."""
    return _redactor
