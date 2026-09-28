package domain

import "regexp"

// loguruPrefix matches loguru-formatted log lines:
//
//	2026-03-25 07:04:46.785 | ERROR    | module:func:258 - actual message
var loguruPrefix = regexp.MustCompile(
	`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}[.,]\d{1,6}\s*\|\s*\w+\s*\|\s*.+?\s*-\s+`,
)

// StripLogPrefix removes common log-framework prefixes (loguru, etc.) from a
// message string, returning just the meaningful content. If no known prefix is
// found the original string is returned unchanged.
func StripLogPrefix(s string) string {
	if m := loguruPrefix.FindStringIndex(s); m != nil {
		return s[m[1]:]
	}
	return s
}
