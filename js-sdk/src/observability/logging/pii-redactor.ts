/**
 * PII redaction for log records.
 * Redacts known sensitive field names and optionally value patterns.
 */

const SENSITIVE_KEYS = new Set([
  'password', 'passwd', 'pwd', 'secret', 'token', 'api_key', 'apikey',
  'authorization', 'cookie', 'session', 'ssn', 'credit_card', 'card_number',
  'email', 'phone', 'mobile', 'address', 'first_name', 'last_name', 'name',
]);

export function redactLogEvent(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(obj)) {
    const keyLower = key.toLowerCase();
    if (SENSITIVE_KEYS.has(keyLower) || keyLower.includes('password') || keyLower.includes('secret')) {
      out[key] = '[REDACTED]';
    } else if (value !== null && typeof value === 'object' && !Array.isArray(value) && !(value instanceof Date)) {
      out[key] = redactLogEvent(value as Record<string, unknown>);
    } else {
      out[key] = value;
    }
  }
  return out;
}
