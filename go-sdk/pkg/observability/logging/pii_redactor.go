package logging

import (
	"regexp"
	"strings"
)

// PIIRedactor handles redaction of sensitive data in log messages
type PIIRedactor struct {
	patterns []*regexp.Regexp
	replaces []string
}

// NewPIIRedactor creates a new redactor with default FinTech patterns
func NewPIIRedactor() *PIIRedactor {
	return &PIIRedactor{
		patterns: []*regexp.Regexp{
			// PAN Card (India): ABCDE1234F
			regexp.MustCompile(`\b[A-Z]{5}[0-9]{4}[A-Z]\b`),
			// Aadhaar (India): 1234 5678 9012 or 123456789012
			regexp.MustCompile(`\b\d{4}\s?\d{4}\s?\d{4}\b`),
			// Credit/Debit Card Numbers: 16 digits with optional spaces/dashes
			regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`),
			// Email addresses
			regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
			// Phone numbers (India): +91 or 0 followed by 10 digits
			regexp.MustCompile(`\b(?:\+91[\s-]?|0)?[6-9]\d{9}\b`),
			// Bank Account Numbers: 9-18 digits
			regexp.MustCompile(`\b\d{9,18}\b`),
			// IFSC Code: 4 letters + 0 + 6 alphanumeric
			regexp.MustCompile(`\b[A-Z]{4}0[A-Z0-9]{6}\b`),
			// Passport Number: Letter followed by 7 digits
			regexp.MustCompile(`\b[A-Z][0-9]{7}\b`),
			// SSN (US): XXX-XX-XXXX
			regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
		replaces: []string{
			"[PAN_REDACTED]",
			"[AADHAAR_REDACTED]",
			"[CARD_REDACTED]",
			"[EMAIL_REDACTED]",
			"[PHONE_REDACTED]",
			"[ACCOUNT_REDACTED]",
			"[IFSC_REDACTED]",
			"[PASSPORT_REDACTED]",
			"[SSN_REDACTED]",
		},
	}
}

// Redact applies all PII redaction patterns to the input string
func (r *PIIRedactor) Redact(input string) string {
	result := input
	for i, pattern := range r.patterns {
		result = pattern.ReplaceAllString(result, r.replaces[i])
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
	"credit_card":    true,
	"card_number":    true,
	"cvv":            true,
	"pin":            true,
	"ssn":            true,
	"aadhaar":        true,
	"pan":            true,
	"account_number": true,
	"routing_number": true,
	"private_key":    true,
}

// IsSensitiveField checks if a field name is sensitive
func IsSensitiveField(fieldName string) bool {
	return SensitiveFields[strings.ToLower(fieldName)]
}
