package logging

import (
	"regexp"
	"strings"
)

// PIIRedactor handles redaction of sensitive data in log messages
type PIIRedactor struct {
	patterns []redactionPattern
}

type redactionPattern struct {
	re   *regexp.Regexp
	mask func(string) string
}

// NewPIIRedactor creates a new redactor with default FinTech patterns
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
				// Account: ******7890
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

// Redact applies all PII redaction patterns to the input string
func (r *PIIRedactor) Redact(input string) string {
	result := input
	for _, p := range r.patterns {
		result = p.re.ReplaceAllStringFunc(result, p.mask)
	}
	return result
}

// RedactMap recursively redacts string values in a map
func (r *PIIRedactor) RedactMap(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		result[k] = r.RedactValue(v)
	}
	return result
}

// RedactValue redacts a single value (handles strings, maps, slices)
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

// SensitiveFields are field names that should be completely masked
var SensitiveFields = map[string]bool{
	"password":       true,
	"secret":         true,
	"token":          true,
	"api_key":        true,
	"apikey":         true,
	"authorization":  true,
	"cvv":            true,
	"pin":            true,
	"private_key":    true,
}

// IsSensitiveField checks if a field name is sensitive
func IsSensitiveField(fieldName string) bool {
	return SensitiveFields[strings.ToLower(fieldName)]
}
