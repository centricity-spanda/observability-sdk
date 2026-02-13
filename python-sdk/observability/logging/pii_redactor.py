"""PII redaction for log messages."""

import re
from typing import Any, Dict, List


class PIIRedactor:
    """Redacts PII from log messages using regex patterns."""

    @staticmethod
    def _mask_pan(match: re.Match) -> str:
        s = match.group(0)
        return s[:5] + "****" + s[9:]

    @staticmethod
    def _mask_aadhaar(match: re.Match) -> str:
        s = match.group(0)
        chars = list(s)
        digit_count = sum(1 for c in chars if c.isdigit())
        seen = 0
        for i, c in enumerate(chars):
            if c.isdigit():
                seen += 1
                if seen <= digit_count - 4:
                    chars[i] = 'X'
        return "".join(chars)

    @staticmethod
    def _mask_card(match: re.Match) -> str:
        s = match.group(0)
        chars = list(s)
        digit_count = sum(1 for c in chars if c.isdigit())
        seen = 0
        for i, c in enumerate(chars):
            if c.isdigit():
                seen += 1
                if seen <= digit_count - 4:
                    chars[i] = '*'
        return "".join(chars)

    @staticmethod
    def _mask_email(match: re.Match) -> str:
        s = match.group(0)
        at_index = s.find("@")
        if at_index > 0:
            prefix = s[:at_index]
            if len(prefix) > 1:
                return prefix[0] + "***" + s[at_index:]
            return "***" + s[at_index:]
        return s

    @staticmethod
    def _mask_phone(match: re.Match) -> str:
        s = match.group(0)
        chars = list(s)
        digit_count = sum(1 for c in chars if c.isdigit())
        seen = 0
        for i, c in enumerate(chars):
            if c.isdigit():
                seen += 1
                if seen <= digit_count - 4:
                    chars[i] = '*'
        return "".join(chars)

    @staticmethod
    def _mask_account(match: re.Match) -> str:
        s = match.group(0)
        if len(s) > 4:
            return "*" * (len(s) - 4) + s[-4:]
        return "*" * len(s)

    @staticmethod
    def _mask_ifsc(match: re.Match) -> str:
        s = match.group(0)
        if len(s) == 11:
            return s[:5] + "******"
        return s

    @staticmethod
    def _mask_passport(match: re.Match) -> str:
        s = match.group(0)
        if len(s) > 2:
            return s[0] + "*" * (len(s) - 2) + s[-1]
        return s

    @staticmethod
    def _mask_ssn(match: re.Match) -> str:
        s = match.group(0)
        chars = list(s)
        digit_count = sum(1 for c in chars if c.isdigit())
        seen = 0
        for i, c in enumerate(chars):
            if c.isdigit():
                seen += 1
                if seen <= digit_count - 4:
                    chars[i] = '*'
        return "".join(chars)

    def __init__(self):
        """Initialize the redactor with updated patterns and maskers."""
        self.SENSITIVE_FIELDS = frozenset({
            'password', 'secret', 'token', 'api_key', 'apikey',
            'authorization', 'cvv', 'pin', 'private_key',
        })
        self.patterns = [
            # PAN Card
            (re.compile(r'\b[A-Z]{5}[0-9]{4}[A-Z]\b'), self._mask_pan),
            # Aadhaar
            (re.compile(r'\b(?:\d{4}\s?\d{4}\s?\d{4})\b'), self._mask_aadhaar),
            # Credit/Debit Card
            (re.compile(r'\b(?:\d[ -]*?){13,19}\b'), self._mask_card),
            # Email
            (re.compile(r'\b[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[A-Za-z]{2,}\b'), self._mask_email),
            # Phone
            (re.compile(r'\b(?:\+?\d{1,3}[-\s]?)?(?:\(?\d{3,5}\)?[-\s]?)?\d{5}[-\s]?\d{5}\b'), self._mask_phone),
            # Bank Account
            (re.compile(r'\b\d{9,18}\b'), self._mask_account),
            # IFSC Code
            (re.compile(r'\b[A-Z]{4}0[A-Z0-9]{6}\b'), self._mask_ifsc),
            # Passport
            (re.compile(r'\b[A-Z][0-9]{7}\b'), self._mask_passport),
            # SSN
            (re.compile(r'\b\d{3}-?\d{2}-?\d{4}\b'), self._mask_ssn),
        ]

    def redact(self, value: str) -> str:
        """Apply all PII redaction patterns to a string."""
        result = value
        for pattern, replacement in self.patterns:
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
