using System;
using System.Collections.Generic;
using System.Text.RegularExpressions;

namespace Centricity.Observability.Logging;

/// <summary>
/// Production-ready PII redaction for log attribute dictionaries.
/// Matches the Go and Python SDK PII redactors with full regex support.
///
/// Redacts: PAN, Aadhaar, credit/debit card, email, phone, bank account,
///          IFSC code, passport, SSN + sensitive field names.
///
/// Skips: URLs, file paths, UUIDs, timestamps, version numbers, business IDs.
/// </summary>
public static class PiiRedactor
{
    // ── Skip patterns — matches inside these spans are NOT treated as PII ────

    private static readonly Regex[] SkipPatterns =
    [
        new(@"https?://[^\s""'<>]+", RegexOptions.Compiled),
        new(@"(?i)(?:[a-z]:\\|\\\\|/)[^\s""'<>]*", RegexOptions.Compiled),
        new(@"\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b", RegexOptions.Compiled),
        new(@"\b(?:20\d{12}|19\d{11}|\d{13}|\d{10})\b", RegexOptions.Compiled),
        new(@"\bv?\d+\.\d+[\.\d\w-]*\b", RegexOptions.Compiled),
        new(@"\b[A-Za-z]{2,}-\d{6,}\b", RegexOptions.Compiled),
    ];

    // ── Non-sensitive allowlist — never redact these fields ──────────────────

    private static readonly HashSet<string> NonSensitiveFields = new(StringComparer.OrdinalIgnoreCase)
    {
        "trace_id", "span_id", "request_id", "correlation_id", "session_id",
        "parent_span_id", "trace_flags",
        "timestamp", "created_at", "updated_at", "event_time", "deleted_at",
        "version", "build", "service", "environment", "caller", "level", "message",
        "order_id", "transaction_id", "invoice_id", "ticket_id", "product_id",
        "item_id", "merchant_id",
        "zipcode", "zip", "pincode", "city", "state", "country", "street",
        "template_url", "file_url", "image_url", "avatar_url", "document_url",
        "url", "uri", "path", "file_path",
        "expiry", "expires_at", "status", "type", "category", "page",
        "limit", "offset", "count", "total",
        "log.type", "team", "severity", "severity_num",
    };

    // ── Sensitive field blocklist — always redact fully ──────────────────────

    private static readonly HashSet<string> SensitiveFields = new(StringComparer.OrdinalIgnoreCase)
    {
        "password", "passwd", "pwd", "secret", "token", "api_key", "apikey",
        "authorization", "cvv", "pin", "private_key",
    };

    // ── PII regex patterns with mask functions ──────────────────────────────

    private static readonly (Regex Pattern, MatchEvaluator Mask)[] PiiRules =
    [
        // PAN Card: ABCDE1234F
        (new Regex(@"\b[A-Z]{5}[0-9]{4}[A-Z]\b", RegexOptions.Compiled),
            m => m.Value.Length == 10 ? m.Value[..5] + "****" + m.Value[9] : m.Value),

        // Aadhaar: 1234 5678 9012
        (new Regex(@"\b(?:\d{4}\s?\d{4}\s?\d{4})\b", RegexOptions.Compiled),
            m => MaskDigitsExceptLast4(m.Value, 'X')),

        // Credit/Debit Card: 4111-1111-1111-1111
        (new Regex(@"\b(?:\d[ -]*?){13,19}\b", RegexOptions.Compiled),
            m => MaskDigitsExceptLast4(m.Value, '*')),

        // Email: user@domain.com → u***@domain.com
        (new Regex(@"\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[A-Za-z]{2,}\b", RegexOptions.Compiled),
            m =>
            {
                var at = m.Value.IndexOf('@');
                if (at <= 0) return m.Value;
                var prefix = m.Value[..at];
                return (prefix.Length > 1 ? prefix[0] + "***" : "***") + m.Value[at..];
            }),

        // Phone: +91-9876543210
        (new Regex(@"\b(?:\+?\d{1,3}[-\s]?)?(?:\(?\d{3,5}\)?[-\s]?)?\d{5}[-\s]?\d{5}\b", RegexOptions.Compiled),
            m => MaskDigitsExceptLast4(m.Value, '*')),

        // Bank Account: 9-18 digit number
        (new Regex(@"\b\d{9,18}\b", RegexOptions.Compiled),
            m => m.Value.Length > 4
                ? new string('*', m.Value.Length - 4) + m.Value[^4..]
                : new string('*', m.Value.Length)),

        // IFSC Code: SBIN0001234
        (new Regex(@"\b[A-Z]{4}0[A-Z0-9]{6}\b", RegexOptions.Compiled),
            m => m.Value.Length == 11 ? m.Value[..5] + "******" : m.Value),

        // Passport: A1234567
        (new Regex(@"\b[A-Z][0-9]{7}\b", RegexOptions.Compiled),
            m => m.Value.Length > 2
                ? m.Value[0] + new string('*', m.Value.Length - 2) + m.Value[^1]
                : m.Value),

        // SSN: 123-45-6789
        (new Regex(@"\b\d{3}-?\d{2}-?\d{4}\b", RegexOptions.Compiled),
            m => MaskDigitsExceptLast4(m.Value, '*')),
    ];

    // ── Public API ──────────────────────────────────────────────────────────

    /// <summary>
    /// Returns a new dictionary with PII-sensitive values masked.
    /// Uses regex patterns for string values and key-based blocking for known fields.
    /// </summary>
    public static Dictionary<string, object?> Redact(Dictionary<string, object?> attrs)
    {
        var result = new Dictionary<string, object?>(attrs.Count, StringComparer.Ordinal);
        foreach (var (key, value) in attrs)
        {
            if (IsSensitiveField(key))
                result[key] = "[REDACTED]";
            else if (IsNonSensitiveField(key))
                result[key] = value;
            else
                result[key] = RedactValue(value);
        }
        return result;
    }

    /// <summary>Redact PII patterns from a single string.</summary>
    public static string RedactString(string input)
    {
        var result = input;
        foreach (var (pattern, mask) in PiiRules)
        {
            result = RedactSmart(result, pattern, mask);
        }
        return result;
    }

    // ── Internals ────────────────────────────────────────────────────────────

    private static object? RedactValue(object? value) => value switch
    {
        string s => RedactString(s),
        Dictionary<string, object?> dict => Redact(dict),
        _ => value,
    };

    private static bool IsSensitiveField(string key) =>
        SensitiveFields.Contains(key) ||
        key.Contains("password", StringComparison.OrdinalIgnoreCase) ||
        key.Contains("secret",   StringComparison.OrdinalIgnoreCase) ||
        key.Contains("token",    StringComparison.OrdinalIgnoreCase);

    private static bool IsNonSensitiveField(string key) =>
        NonSensitiveFields.Contains(key);

    private static bool IsSkippable(string input, int start, int end)
    {
        foreach (var pattern in SkipPatterns)
        {
            foreach (Match m in pattern.Matches(input))
            {
                if (start >= m.Index && end <= m.Index + m.Length)
                    return true;
            }
        }
        return false;
    }

    private static string RedactSmart(string input, Regex pattern, MatchEvaluator mask)
    {
        // Find all matches and apply mask only if not inside a skip zone
        return pattern.Replace(input, m =>
            IsSkippable(input, m.Index, m.Index + m.Length) ? m.Value : mask(m));
    }

    private static string MaskDigitsExceptLast4(string s, char maskChar)
    {
        var chars = s.ToCharArray();
        int digitCount = 0;
        foreach (var c in chars) if (char.IsDigit(c)) digitCount++;

        int seen = 0;
        for (int i = 0; i < chars.Length; i++)
        {
            if (char.IsDigit(chars[i]))
            {
                seen++;
                if (seen <= digitCount - 4)
                    chars[i] = maskChar;
            }
        }
        return new string(chars);
    }
}
