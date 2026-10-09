package domain

import (
	"errors"
	"testing"
)

func TestCentsFromFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want Cents
	}{
		{35.5, 3550},
		{0.65, 65},
		{12.91, 1291},
		{8.99, 899},
		{0.005, 1},
		{0, 0},
		{-1.05, -105},
	}
	for _, tt := range tests {
		if got := CentsFromFloat(tt.in); got != tt.want {
			t.Errorf("CentsFromFloat(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseCents(t *testing.T) {
	tests := []struct {
		in      string
		want    Cents
		wantErr bool
	}{
		{"12.30", 1230, false},
		{"12.3", 1230, false},
		{"12", 1200, false},
		{" 500.00 ", 50000, false},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseCents(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestCentsFormat(t *testing.T) {
	tests := []struct {
		in       Cents
		str, sgd string
	}{
		{3550, "35.50", "S$35.50"},
		{5, "0.05", "S$0.05"},
		{-105, "-1.05", "-S$1.05"},
	}
	for _, tt := range tests {
		if tt.in.String() != tt.str || tt.in.SGD() != tt.sgd {
			t.Errorf("%d: got %q %q", tt.in, tt.in.String(), tt.in.SGD())
		}
	}
	if Cents(710).MulQty(10) != 7100 {
		t.Error("MulQty")
	}
}

func TestStateMachine(t *testing.T) {
	tests := []struct {
		from, to RequestStatus
		ok       bool
	}{
		{StatusParsing, StatusSearching, true},
		{StatusSearching, StatusQuoted, true},
		{StatusQuoted, StatusApproved, true},
		{StatusQuoted, StatusPendingApproval, true},
		{StatusQuoted, StatusRejected, true},
		{StatusPendingApproval, StatusApproved, true},
		{StatusPendingApproval, StatusRejected, true},
		{StatusApproved, StatusCheckingOut, true},
		{StatusCheckingOut, StatusPendingApproval, true}, // price drift
		{StatusCheckingOut, StatusAwaitingPayment, true},
		{StatusAwaitingPayment, StatusPaying, true},
		{StatusAwaitingPayment, StatusOrdered, true},
		{StatusPaying, StatusOrdered, true},
		{StatusPaying, StatusCancelled, false}, // money may be moving
		{StatusQuoted, StatusOrdered, false},
		{StatusParsing, StatusApproved, false},
		{StatusOrdered, StatusFailed, false},
		{StatusRejected, StatusApproved, false},
	}
	for _, tt := range tests {
		if got := CanTransition(tt.from, tt.to); got != tt.ok {
			t.Errorf("CanTransition(%s,%s) = %v, want %v", tt.from, tt.to, got, tt.ok)
		}
		err := CheckTransition(tt.from, tt.to)
		if tt.ok != (err == nil) || (!tt.ok && !errors.Is(err, ErrInvalidTransition)) {
			t.Errorf("CheckTransition(%s,%s) = %v", tt.from, tt.to, err)
		}
	}
	for _, s := range AllStatuses {
		if !s.Valid() {
			t.Errorf("%s not valid", s)
		}
		if s != StatusOrdered && s != StatusRejected && s != StatusFailed && s != StatusCancelled && s.Terminal() {
			t.Errorf("%s should not be terminal", s)
		}
		if s.Terminal() {
			continue
		}
		if s != StatusPaying && !CanTransition(s, StatusFailed) {
			t.Errorf("%s should be able to fail", s)
		}
	}
	if RequestStatus("bogus").Valid() {
		t.Error("bogus valid")
	}
}

func TestRoles(t *testing.T) {
	if RoleManager.CanApprove() || !RoleApprover.CanApprove() || !RoleAdmin.CanApprove() {
		t.Error("CanApprove")
	}
	if RoleApprover.CanAdmin() || !RoleAdmin.CanAdmin() {
		t.Error("CanAdmin")
	}
}
