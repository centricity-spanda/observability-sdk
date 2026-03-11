/**
 * Production-ready PII redaction for log records.
 * Matches the Go and Python SDK PII redactors with full regex pattern support.
 *
 * Redacts: PAN, Aadhaar, credit/debit card, email, phone, bank account,
 *          IFSC code, passport, SSN + sensitive field names.
 *
 * Skips: URLs, file paths, UUIDs, timestamps, version numbers, business IDs.
 */

// ── Skip patterns — matches inside these are NOT treated as PII ──────────

const SKIP_PATTERNS: RegExp[] = [
  /https?:\/\/[^\s"'<>]+/g,                                                   // URLs
  /(?:[a-zA-Z]:\\|\\\\|\/)[^\s"'<>]*/gi,                                       // File paths
  /\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b/g, // UUIDs
  /\b(?:20\d{12}|19\d{11}|\d{13}|\d{10})\b/g,                                  // Timestamps
  /\bv?\d+\.\d+[\.\d\w-]*\b/g,                                                 // Versions
  /\b[A-Za-z]{2,}-\d{6,}\b/g,                                                  // Business IDs
];

function isSkippable(input: string, start: number, end: number): boolean {
  for (const pattern of SKIP_PATTERNS) {
    pattern.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = pattern.exec(input)) !== null) {
      if (start >= m.index && end <= m.index + m[0].length) return true;
    }
  }
  return false;
}

// ── Non-sensitive allowlist — never redact values for these keys ──────────

const NON_SENSITIVE_FIELDS = new Set([
  'trace_id', 'span_id', 'request_id', 'correlation_id', 'session_id',
  'parent_span_id', 'trace_flags',
  'timestamp', 'created_at', 'updated_at', 'event_time', 'deleted_at',
  'version', 'build', 'service', 'environment', 'caller', 'level', 'message',
  'order_id', 'transaction_id', 'invoice_id', 'ticket_id', 'product_id',
  'item_id', 'merchant_id',
  'zipcode', 'zip', 'pincode', 'city', 'state', 'country', 'street',
  'template_url', 'file_url', 'image_url', 'avatar_url', 'document_url',
  'url', 'uri', 'path', 'file_path',
  'expiry', 'expires_at', 'status', 'type', 'category', 'page',
  'limit', 'offset', 'count', 'total',
  'log.type', 'team', 'severity', 'severity_num',
  'service.name', 'service.version', 'service.namespace',
  'deployment.environment', 'host.name',
  'k8s.pod.name', 'k8s.namespace.name', 'k8s.node.name',
]);

// ── Sensitive field blocklist — always redact these keys ──────────────────

const SENSITIVE_FIELDS = new Set([
  'password', 'passwd', 'pwd', 'secret', 'token', 'api_key', 'apikey',
  'authorization', 'cvv', 'pin', 'private_key',
]);

function isSensitiveField(key: string): boolean {
  const k = key.toLowerCase();
  return SENSITIVE_FIELDS.has(k) ||
    k.includes('password') || k.includes('secret') || k.includes('token');
}

function isNonSensitiveField(key: string): boolean {
  return NON_SENSITIVE_FIELDS.has(key.toLowerCase());
}

// ── Regex patterns and mask functions ────────────────────────────────────

interface PIIRule {
  pattern: RegExp;
  mask: (match: RegExpExecArray) => string;
}

function maskPAN(m: RegExpExecArray): string {
  const s = m[0];
  return s.length === 10 ? s.slice(0, 5) + '****' + s[9] : s;
}

function maskAadhaar(m: RegExpExecArray): string {
  const s = m[0];
  const chars = s.split('');
  let digitCount = 0;
  for (const c of chars) if (/\d/.test(c)) digitCount++;
  let seen = 0;
  for (let i = 0; i < chars.length; i++) {
    if (/\d/.test(chars[i])) {
      seen++;
      if (seen <= digitCount - 4) chars[i] = 'X';
    }
  }
  return chars.join('');
}

function maskCard(m: RegExpExecArray): string {
  const s = m[0];
  const chars = s.split('');
  let digitCount = 0;
  for (const c of chars) if (/\d/.test(c)) digitCount++;
  let seen = 0;
  for (let i = 0; i < chars.length; i++) {
    if (/\d/.test(chars[i])) {
      seen++;
      if (seen <= digitCount - 4) chars[i] = '*';
    }
  }
  return chars.join('');
}

function maskEmail(m: RegExpExecArray): string {
  const s = m[0];
  const at = s.indexOf('@');
  if (at > 0) {
    const prefix = s.slice(0, at);
    return (prefix.length > 1 ? prefix[0] + '***' : '***') + s.slice(at);
  }
  return s;
}

function maskPhone(m: RegExpExecArray): string {
  const s = m[0];
  const chars = s.split('');
  let digitCount = 0;
  for (const c of chars) if (/\d/.test(c)) digitCount++;
  let seen = 0;
  for (let i = 0; i < chars.length; i++) {
    if (/\d/.test(chars[i])) {
      seen++;
      if (seen <= digitCount - 4) chars[i] = '*';
    }
  }
  return chars.join('');
}

function maskAccount(m: RegExpExecArray): string {
  const s = m[0];
  return s.length > 4 ? '*'.repeat(s.length - 4) + s.slice(-4) : '*'.repeat(s.length);
}

function maskIFSC(m: RegExpExecArray): string {
  const s = m[0];
  return s.length === 11 ? s.slice(0, 5) + '******' : s;
}

function maskPassport(m: RegExpExecArray): string {
  const s = m[0];
  return s.length > 2 ? s[0] + '*'.repeat(s.length - 2) + s[s.length - 1] : s;
}

function maskSSN(m: RegExpExecArray): string {
  const s = m[0];
  const chars = s.split('');
  let digitCount = 0;
  for (const c of chars) if (/\d/.test(c)) digitCount++;
  let seen = 0;
  for (let i = 0; i < chars.length; i++) {
    if (/\d/.test(chars[i])) {
      seen++;
      if (seen <= digitCount - 4) chars[i] = '*';
    }
  }
  return chars.join('');
}

const PII_RULES: PIIRule[] = [
  { pattern: /\b[A-Z]{5}[0-9]{4}[A-Z]\b/g,                                    mask: maskPAN     },
  { pattern: /\b(?:\d{4}\s?\d{4}\s?\d{4})\b/g,                                 mask: maskAadhaar },
  { pattern: /\b(?:\d[ -]*?){13,19}\b/g,                                        mask: maskCard    },
  { pattern: /\b[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[A-Za-z]{2,}\b/g,            mask: maskEmail   },
  { pattern: /\b(?:\+?\d{1,3}[-\s]?)?(?:\(?\d{3,5}\)?[-\s]?)?\d{5}[-\s]?\d{5}\b/g, mask: maskPhone},
  { pattern: /\b\d{9,18}\b/g,                                                   mask: maskAccount },
  { pattern: /\b[A-Z]{4}0[A-Z0-9]{6}\b/g,                                       mask: maskIFSC    },
  { pattern: /\b[A-Z][0-9]{7}\b/g,                                              mask: maskPassport},
  { pattern: /\b\d{3}-?\d{2}-?\d{4}\b/g,                                        mask: maskSSN     },
];

// ── Smart redaction engine ───────────────────────────────────────────────

function redactSmart(input: string, pattern: RegExp, maskFn: (m: RegExpExecArray) => string): string {
  pattern.lastIndex = 0;
  const parts: string[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(input)) !== null) {
    parts.push(input.slice(lastIndex, match.index));
    if (isSkippable(input, match.index, match.index + match[0].length)) {
      parts.push(match[0]); // safe zone — preserve
    } else {
      parts.push(maskFn(match)); // apply mask
    }
    lastIndex = match.index + match[0].length;
  }
  parts.push(input.slice(lastIndex));
  return parts.join('');
}

/** Redact PII patterns from a string value. */
export function redactString(input: string): string {
  let result = input;
  for (const rule of PII_RULES) {
    result = redactSmart(result, rule.pattern, rule.mask);
  }
  return result;
}

/** Recursively redact PII from a log event dictionary. */
export function redactLogEvent(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    if (isSensitiveField(key)) {
      out[key] = '[REDACTED]';
    } else if (isNonSensitiveField(key)) {
      out[key] = value; // operational field — pass through
    } else {
      out[key] = redactValue(value);
    }
  }
  return out;
}

function redactValue(value: unknown): unknown {
  if (typeof value === 'string') return redactString(value);
  if (Array.isArray(value)) return value.map(redactValue);
  if (value !== null && typeof value === 'object' && !(value instanceof Date)) {
    return redactLogEvent(value as Record<string, unknown>);
  }
  return value;
}
