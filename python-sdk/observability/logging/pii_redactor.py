"""PII redaction for log messages."""

import re
from typing import Any, Dict, List, Set, Union

# ---------------------------------------------------------------------------
# Skip patterns — matches that fall inside these spans are NEVER treated as PII
# ---------------------------------------------------------------------------

SKIP_PATTERNS = [
    # 1. http / https URLs
    re.compile(r'https?://[^\s"\'<>]+'),
    # 2. Local and UNC file paths
    re.compile(r'(?i)(?:[a-z]:\\|\\\\|/)(?:[^\s"\'<>]*)'),
    # 3. Full UUID
    re.compile(r'\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b'),
    # 4. Epoch timestamps and compact datetimes
    re.compile(r'\b(?:20\d{12}|19\d{11}|\d{13}|\d{10})\b'),
    # 5. Semantic version / build numbers
    re.compile(r'\bv?\d+\.\d+[\.\d\w-]*\b'),
    # 6. Prefixed business IDs
    re.compile(r'\b[A-Za-z]{2,}-\d{6,}\b'),
]

def is_skippable(input_str: str, start: int, end: int) -> bool:
    """Check if the match at [start, end] falls entirely within any skip-pattern span."""
    for pattern in SKIP_PATTERNS:
        for match in pattern.finditer(input_str):
            if start >= match.start() and end <= match.end():
                return True
    return False

# ---------------------------------------------------------------------------
# Non-sensitive field allowlist
# ---------------------------------------------------------------------------

NON_SENSITIVE_FIELDS = {
    # Tracing / correlation
    "trace_id", "span_id", "request_id", "correlation_id", "session_id",
    # Timestamps
    "timestamp", "created_at", "updated_at", "event_time", "deleted_at",
    # Application metadata
    "version", "build", "service", "environment", "caller", "level", "message",
    # Business / order identifiers (non-personal)
    "order_id", "transaction_id", "invoice_id", "ticket_id", "product_id",
    "item_id", "merchant_id",
    # Address structural fields
    "zipcode", "zip", "pincode", "city", "state", "country", "street",
    # File / media references
    "template_url", "file_url", "image_url", "avatar_url", "document_url",
    "url", "uri", "path", "file_path",
    # Generic safe fields
    "expiry", "expires_at", "status", "type", "category", "page",
    "limit", "offset", "count", "total",
}

def is_non_sensitive_field(field_name: str) -> bool:
    return field_name.lower() in NON_SENSITIVE_FIELDS

# ---------------------------------------------------------------------------
# Sensitive field blocklist
# ---------------------------------------------------------------------------

SENSITIVE_FIELDS = {
    'password', 'secret', 'token', 'api_key', 'apikey',
    'authorization', 'cvv', 'pin', 'private_key',
}

def is_sensitive_field(field_name: str) -> bool:
    return field_name.lower() in SENSITIVE_FIELDS


# ---------------------------------------------------------------------------
# Core redaction engine
# ---------------------------------------------------------------------------

def redact_smart(input_str: str, pattern: re.Pattern, mask_func) -> str:
    """Applies re+mask to every match in input that is NOT covered by any skip pattern."""
    result = []
    last_index = 0
    
    # We find all matches first
    matches = list(pattern.finditer(input_str))
    
    for match in matches:
        start, end = match.span()
        
        # Append text before this match
        result.append(input_str[last_index:start])
        
        matched_text = match.group(0)
        
        if is_skippable(input_str, start, end):
            result.append(matched_text) # not PII — preserve verbatim
        else:
            result.append(mask_func(match)) # Apply mask function
            
        last_index = end
        
    result.append(input_str[last_index:])
    return "".join(result)


class PIIRedactor:
    """Redacts PII from log messages using regex patterns."""

    @staticmethod
    def _mask_pan(match: re.Match) -> str:
        s = match.group(0)
        if len(s) == 10:
             return s[:5] + "****" + s[9:]
        return s

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

    def redact(self, input_str: str) -> str:
        """Apply all PII redaction patterns to a string, skipping safe zones."""
        result = input_str
        for pattern, mask_func in self.patterns:
            result = redact_smart(result, pattern, mask_func)
        return result

    def redact_dict(self, data: Dict[str, Any]) -> Dict[str, Any]:
        """Recursively redact PII from a dictionary."""
        result = {}
        for key, value in data.items():
            if is_sensitive_field(key):
                 result[key] = '[REDACTED]'
            elif is_non_sensitive_field(key):
                 result[key] = value # structural / operational — pass through
            else:
                 result[key] = self.redact_value(value)
        return result
    
    def redact_value(self, value: Any) -> Any:
        """Redact a single value based on type."""
        if isinstance(value, str):
            return self.redact(value)
        elif isinstance(value, dict):
            return self.redact_dict(value)
        elif isinstance(value, list):
            return [self.redact_value(item) for item in value]
        elif isinstance(value, tuple):
            return tuple(self.redact_value(item) for item in value)
        elif isinstance(value, set):
            return {self.redact_value(item) for item in value}
        return value


# Global redactor instance
_redactor = PIIRedactor()


def redact_log_event(event_dict: Dict[str, Any]) -> Dict[str, Any]:
    """Redact PII from a structlog event dictionary."""
    return _redactor.redact_dict(event_dict)


def get_redactor() -> PIIRedactor:
    """Get the global PII redactor instance."""
    return _redactor
