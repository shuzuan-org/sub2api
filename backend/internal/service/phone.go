package service

import (
	"regexp"
	"strings"
)

// phoneCNPattern matches Chinese mobile numbers: 1[3-9]XXXXXXXXX
var phoneCNPattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

// NormalizePhoneNumber trims, validates, and normalizes a Chinese phone number
// to E.164 format (+8613800138000).
// Returns ErrInvalidPhoneNumber if the input does not match a valid Chinese mobile number.
func NormalizePhoneNumber(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidPhoneNumber
	}

	// Strip leading "+86", "+", "86", spaces, and hyphens
	normalized := raw
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.TrimPrefix(normalized, "+86")
	normalized = strings.TrimPrefix(normalized, "86")
	normalized = strings.TrimPrefix(normalized, "+")

	if !phoneCNPattern.MatchString(normalized) {
		return "", ErrInvalidPhoneNumber
	}
	return "+86" + normalized, nil
}

// NormalizePhoneNumberOrEmpty normalizes a phone number and returns empty string on error.
func NormalizePhoneNumberOrEmpty(raw string) string {
	normalized, err := NormalizePhoneNumber(raw)
	if err != nil {
		return ""
	}
	return normalized
}
