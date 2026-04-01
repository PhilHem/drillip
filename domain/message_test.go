package domain

import "testing"

func TestStripLogPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "loguru format",
			input: "2026-03-25 07:04:46.785 | ERROR    | entitlements_app.adapters.portal2:_login:258 - Portal2 role switch failed",
			want:  "Portal2 role switch failed",
		},
		{
			name:  "loguru with milliseconds comma",
			input: "2026-03-25 07:04:46,785 | WARNING  | mymodule:func:10 - disk full",
			want:  "disk full",
		},
		{
			name:  "loguru microseconds",
			input: "2026-03-25 07:04:46.785123 | DEBUG    | mod:f:1 - msg",
			want:  "msg",
		},
		{
			name:  "plain message unchanged",
			input: "Portal2 role switch failed",
			want:  "Portal2 role switch failed",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "message with date but not loguru",
			input: "2026-03-25 some random text",
			want:  "2026-03-25 some random text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripLogPrefix(tt.input)
			if got != tt.want {
				t.Errorf("StripLogPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
