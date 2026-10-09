package domain

import "testing"

func TestReservedEmailDomain(t *testing.T) {
	tests := []struct {
		email string
		want  bool
	}{
		{"maya.tan@jarvis-office.example", true},
		{"maya.tan@JARVIS-OFFICE.EXAMPLE.", true},
		{"a@b.test", true},
		{"a@host.invalid", true},
		{"a@localhost", true},
		{"a@printer.local", true},
		{"maya.tan@corp.example.com", false},
		{"jane@example.com", false},
		{"ops@company.sg", false},
		{"no-at-sign", false},
	}
	for _, tc := range tests {
		t.Run(tc.email, func(t *testing.T) {
			if got := ReservedEmailDomain(tc.email); got != tc.want {
				t.Fatalf("ReservedEmailDomain(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}
