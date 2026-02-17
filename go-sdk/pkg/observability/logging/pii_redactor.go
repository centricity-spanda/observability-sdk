package logging

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Skip patterns — matches that fall inside these spans are NEVER treated as PII
// ---------------------------------------------------------------------------
//
// Rationale for each entry:
//
//  1. URLs          — numeric segments in S3 keys, CDN paths, query params, etc.
//  2. File paths    — Unix (/dir/12345_file.pdf) and Windows (C:\dir\12345.pdf)
//  3. UUIDs         — trace/request IDs that happen to contain long digit runs
//  4. Epoch / compact timestamps — 10-digit (Unix s), 13-digit (ms), 14-digit (YYYYMMDDHHmmss)
//  5. Semver / build strings — "0.0.1", "2.3.14-rc1"
//  6. Prefixed business IDs   — "ORD-123456789", "TXN987654321" (alpha prefix + digits)
//
// NOTE: Zip/postal codes that share the \d{3}-\d{2}-\d{4} SSN pattern cannot
// be distinguished from SSNs in free text.  Protection for those is handled at
// the key level via NonSensitiveFields ("zipcode", "zip", "pincode").

var skipPatterns = []*regexp.Regexp{
	// 1. http / https URLs
	regexp.MustCompile(`https?://[^\s"'<>]+`),

	// 2. Local and UNC file paths
	regexp.MustCompile(`(?i)(?:[a-z]:\\|\\\\|/)(?:[^\s"'<>]*)`),

	// 3. Full UUID  e.g. 6ba7b810-9dad-11d1-80b4-00c04fd430c8
	regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`),

	// 4. Epoch timestamps and compact datetimes
	//    10-digit Unix seconds | 13-digit ms | 14-digit YYYYMMDDHHmmss
	regexp.MustCompile(`\b(?:20\d{12}|19\d{11}|\d{13}|\d{10})\b`),

	// 5. Semantic version / build numbers  e.g. 1.0.0  2.3.14-rc1  v0.0.1
	regexp.MustCompile(`\bv?\d+\.\d+[\.\d\w-]*\b`),

	// 6. Prefixed business IDs  e.g. ORD-123456789012  TXN987654321098
	regexp.MustCompile(`\b[A-Za-z]{2,}-\d{6,}\b`),
}

// isSkippable returns true when the match at [start, end] inside input falls
// entirely within any skip-pattern span.
func isSkippable(input string, start, end int) bool {
	for _, sp := range skipPatterns {
		for _, span := range sp.FindAllStringIndex(input, -1) {
			if start >= span[0] && end <= span[1] {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Non-sensitive field allowlist
// ---------------------------------------------------------------------------
//
// Values of keys in this map are structural / operational data — they are
// passed through RedactMap completely untouched (no pattern matching).
// This is the correct place to protect things like zip codes whose numeric
// format could collide with PII patterns.
//
// Add new keys here as your schema grows.

var NonSensitiveFields = map[string]bool{
	// Tracing / correlation
	"trace_id":       true,
	"span_id":        true,
	"request_id":     true,
	"correlation_id": true,
	"session_id":     true,

	// Timestamps
	"timestamp":  true,
	"created_at": true,
	"updated_at": true,
	"event_time": true,
	"deleted_at": true,

	// Application metadata
	"version":     true,
	"build":       true,
	"service":     true,
	"environment": true,
	"caller":      true,
	"level":       true,
	"message":     true,

	// Business / order identifiers (non-personal)
	"order_id":       true,
	"transaction_id": true,
	"invoice_id":     true,
	"ticket_id":      true,
	"product_id":     true,
	"item_id":        true,
	"merchant_id":    true,

	// Address structural fields
	// These also guard zip/postal codes from the SSN pattern.
	"zipcode": true,
	"zip":     true,
	"pincode": true,
	"city":    true,
	"state":   true,
	"country": true,
	"street":  true,

	// File / media references
	"template_url": true,
	"file_url":     true,
	"image_url":    true,
	"avatar_url":   true,
	"document_url": true,
	"url":          true,
	"uri":          true,
	"path":         true,
	"file_path":    true,

	// Generic safe fields
	"expiry":     true,
	"expires_at": true,
	"status":     true,
	"type":       true,
	"category":   true,
	"page":       true,
	"limit":      true,
	"offset":     true,
	"count":      true,
	"total":      true,
}

// IsNonSensitiveField checks if a field name is a known safe / structural field.
func IsNonSensitiveField(fieldName string) bool {
	return NonSensitiveFields[strings.ToLower(fieldName)]
}

// ---------------------------------------------------------------------------
// Core redaction engine
// ---------------------------------------------------------------------------

// redactSmart applies re+mask to every match in input that is NOT covered
// by any skip pattern.
func redactSmart(input string, re *regexp.Regexp, mask func(string) string) string {
	var buf strings.Builder
	lastIndex := 0
	for _, loc := range re.FindAllStringIndex(input, -1) {
		start, end := loc[0], loc[1]
		buf.WriteString(input[lastIndex:start])
		match := input[start:end]
		if isSkippable(input, start, end) {
			buf.WriteString(match) // not PII — preserve verbatim
		} else {
			buf.WriteString(mask(match))
		}
		lastIndex = end
	}
	buf.WriteString(input[lastIndex:])
	return buf.String()
}

// ---------------------------------------------------------------------------
// PIIRedactor
// ---------------------------------------------------------------------------

// PIIRedactor handles redaction of sensitive data in log messages.
type PIIRedactor struct {
	patterns []redactionPattern
}

type redactionPattern struct {
	re   *regexp.Regexp
	mask func(string) string
}

// NewPIIRedactor creates a new redactor with default FinTech patterns.
func NewPIIRedactor() *PIIRedactor {
	return &PIIRedactor{
		patterns: []redactionPattern{
			{
				// PAN Card (India): ABCDE1234F -> ABCDE****F
				re: regexp.MustCompile(`\b[A-Z]{5}[0-9]{4}[A-Z]\b`),
				mask: func(s string) string {
					if len(s) == 10 {
						return s[:5] + "****" + s[9:]
					}
					return s
				},
			},
			{
				// Aadhaar (India): 1234 5678 9012 -> XXXX XXXX 1234
				re: regexp.MustCompile(`\b(?:\d{4}\s?\d{4}\s?\d{4})\b`),
				mask: func(s string) string {
					rs := []rune(s)
					digitCount := 0
					for _, r := range rs {
						if r >= '0' && r <= '9' {
							digitCount++
						}
					}
					seen := 0
					for i, r := range rs {
						if r >= '0' && r <= '9' {
							seen++
							if seen <= digitCount-4 {
								rs[i] = 'X'
							}
						}
					}
					return string(rs)
				},
			},
			{
				// Credit/Debit Card: ************1234
				re: regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
				mask: func(s string) string {
					rs := []rune(s)
					digitCount := 0
					for _, r := range rs {
						if r >= '0' && r <= '9' {
							digitCount++
						}
					}
					seen := 0
					for i, r := range rs {
						if r >= '0' && r <= '9' {
							seen++
							if seen <= digitCount-4 {
								rs[i] = '*'
							}
						}
					}
					return string(rs)
				},
			},
			{
				// Email: j***@domain.com
				re: regexp.MustCompile(`\b[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[A-Za-z]{2,}\b`),
				mask: func(s string) string {
					at := strings.Index(s, "@")
					if at > 0 {
						prefix := s[:at]
						if len(prefix) > 1 {
							return string(prefix[0]) + "***" + s[at:]
						}
						return "***" + s[at:]
					}
					return s
				},
			},
			{
				// Phone: ******3210
				re: regexp.MustCompile(`\b(?:\+?\d{1,3}[-\s]?)?(?:\(?\d{3,5}\)?[-\s]?)?\d{5}[-\s]?\d{5}\b`),
				mask: func(s string) string {
					rs := []rune(s)
					digitCount := 0
					for _, r := range rs {
						if r >= '0' && r <= '9' {
							digitCount++
						}
					}
					seen := 0
					for i, r := range rs {
						if r >= '0' && r <= '9' {
							seen++
							if seen <= digitCount-4 {
								rs[i] = '*'
							}
						}
					}
					return string(rs)
				},
			},
			{
				// Account number: ******7890
				re: regexp.MustCompile(`\b\d{9,18}\b`),
				mask: func(s string) string {
					if len(s) > 4 {
						return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
					}
					return strings.Repeat("*", len(s))
				},
			},
			{
				// IFSC: SBIN0*****
				re: regexp.MustCompile(`\b[A-Z]{4}0[A-Z0-9]{6}\b`),
				mask: func(s string) string {
					if len(s) == 11 {
						return s[:5] + "******"
					}
					return s
				},
			},
			{
				// Passport: A******7
				re: regexp.MustCompile(`\b[A-Z][0-9]{7}\b`),
				mask: func(s string) string {
					if len(s) > 2 {
						return string(s[0]) + strings.Repeat("*", len(s)-2) + string(s[len(s)-1])
					}
					return s
				},
			},
			{
				// SSN: ***-**-6789
				re: regexp.MustCompile(`\b\d{3}-?\d{2}-?\d{4}\b`),
				mask: func(s string) string {
					rs := []rune(s)
					digitCount := 0
					for _, r := range rs {
						if r >= '0' && r <= '9' {
							digitCount++
						}
					}
					seen := 0
					for i, r := range rs {
						if r >= '0' && r <= '9' {
							seen++
							if seen <= digitCount-4 {
								rs[i] = '*'
							}
						}
					}
					return string(rs)
				},
			},
		},
	}
}

// Redact applies all PII patterns to input, skipping any match that falls
// inside a URL, file path, UUID, timestamp, version string, or prefixed ID.
func (r *PIIRedactor) Redact(input string) string {
	result := input
	for _, p := range r.patterns {
		result = redactSmart(result, p.re, p.mask)
	}
	return result
}

// RedactMap recursively redacts string values in a map.
//
//   - SensitiveFields    → always fully replaced with [REDACTED]
//   - NonSensitiveFields → passed through untouched (no pattern matching)
//   - Everything else    → pattern-matched via Redact()
func (r *PIIRedactor) RedactMap(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		switch {
		case IsSensitiveField(k):
			result[k] = "[REDACTED]"
		case IsNonSensitiveField(k):
			result[k] = v // structural / operational — pass through
		default:
			result[k] = r.RedactValue(v)
		}
	}
	return result
}

// RedactValue redacts a single value (handles strings, maps, slices).
func (r *PIIRedactor) RedactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return r.Redact(val)
	case map[string]interface{}:
		return r.RedactMap(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = r.RedactValue(item)
		}
		return result
	default:
		return v
	}
}

// ---------------------------------------------------------------------------
// Field classification helpers
// ---------------------------------------------------------------------------

// SensitiveFields are field names whose values must always be fully masked.
var SensitiveFields = map[string]bool{
	"password":      true,
	"secret":        true,
	"token":         true,
	"api_key":       true,
	"apikey":        true,
	"authorization": true,
	"cvv":           true,
	"pin":           true,
	"private_key":   true,
}

// IsSensitiveField checks if a field name is sensitive.
func IsSensitiveField(fieldName string) bool {
	return SensitiveFields[strings.ToLower(fieldName)]
}