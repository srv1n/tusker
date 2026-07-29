package main

import "testing"

func TestErrorToIssueNil(t *testing.T) {
	if issue := errorToIssue(nil); issue != (Issue{}) {
		t.Fatalf("nil error issue=%#v", issue)
	}
}
