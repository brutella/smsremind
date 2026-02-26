package main

import (
	"os"
	"reflect"
	"testing"
)

func TestParseCalendarNames(t *testing.T) {
	got := parseCalendarNames(" Work, Private ,, Team ")
	want := []string{"Work", "Private", "Team"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCalendarNames = %#v, want %#v", got, want)
	}
}

func TestRequireEnv(t *testing.T) {
	key := "SMSREMIND_TEST_REQUIRE_ENV"
	_ = os.Unsetenv(key)
	if _, err := RequireEnv(key); err == nil {
		t.Fatalf("expected error when env missing")
	}
	if err := os.Setenv(key, "ok"); err != nil {
		t.Fatalf("Setenv error = %v", err)
	}
	defer os.Unsetenv(key)
	v, err := RequireEnv(key)
	if err != nil {
		t.Fatalf("RequireEnv error = %v", err)
	}
	if v != "ok" {
		t.Fatalf("value = %q, want ok", v)
	}
}
