package test

import (
	"regexp"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
)

// AssertNoError will fail if the provided err value is an error.
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// AssertErrorMatch will fail if the error doesn't match the provided error.
func AssertErrorMatch(t *testing.T, s string, e error) {
	t.Helper()
	if !MatchErrorString(t, s, e) {
		t.Fatalf("error did not match, got %s, want %s", e, s)
	}
}

// AssertNotFound will fail if the provided err value not a NotFound error from
// the K8s client API.
func AssertNotFound(t *testing.T, err error) {
	t.Helper()

	if !errors.IsNotFound(err) {
		t.Fatalf("got err %v instead of NotFound", err)
	}
}

// MatchErrorString takes a string and matches on the error and returns true if the
// string matches the error.
//
// This is useful in table tests.
//
// If the string can't be compiled as an regexp, then this will fail with a
// Fatal error.
func MatchErrorString(t *testing.T, s string, e error) bool {
	t.Helper()
	if s == "" && e == nil {
		return true
	}
	if s != "" && e == nil {
		return false
	}
	match, err := regexp.MatchString(s, e.Error())
	if err != nil {
		t.Fatal(err)
	}
	return match
}
