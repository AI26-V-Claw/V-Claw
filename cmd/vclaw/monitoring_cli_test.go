package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseSinceDuration(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.Local)
	got, err := parseSince("1h", now)
	if err != nil {
		t.Fatalf("parseSince: %v", err)
	}
	want := now.Add(-1 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("parseSince = %v, want %v", got, want)
	}
}

func TestParseSinceDaysDuration(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.Local)
	got, err := parseSince("7d", now)
	if err != nil {
		t.Fatalf("parseSince: %v", err)
	}
	want := now.Add(-7 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("parseSince = %v, want %v", got, want)
	}
}

func TestParseSinceDateOnly(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.Local)
	got, err := parseSince("2026-06-01", now)
	if err != nil {
		t.Fatalf("parseSince: %v", err)
	}
	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("parseSince = %v, want %v", got, want)
	}
}

func TestParseSinceDateTimeNoTimezone(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.Local)
	got, err := parseSince("2026-06-01T15:04:05", now)
	if err != nil {
		t.Fatalf("parseSince: %v", err)
	}
	want := time.Date(2026, 6, 1, 15, 4, 5, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("parseSince = %v, want %v", got, want)
	}
}

func TestParseSinceRFC3339(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.Local)
	got, err := parseSince("2026-06-01T15:04:05Z", now)
	if err != nil {
		t.Fatalf("parseSince: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-06-01T15:04:05Z")
	if !got.Equal(want) {
		t.Fatalf("parseSince = %v, want %v", got, want)
	}
}

func TestParseSinceInvalid(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.Local)
	_, err := parseSince("banana", now)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Giá trị --since không hợp lệ") {
		t.Fatalf("unexpected error: %v", err)
	}
}
