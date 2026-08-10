package main

import (
	"strings"
	"testing"
)

func TestHookOutputIsBoundedAndRedacted(t *testing.T) {
	output := &boundedHookOutput{}
	secretCorpus := strings.Join([]string{
		`token: "quoted secret value"`,
		`password=space separated value`,
		`{"api_key":"json-secret","safe":"kept"}`,
		`Authorization: Bearer bearer-secret-value`,
		`https://example.test/?access_token=query-secret&safe=1`,
		`ghp_abcdefghijklmnopqrstuvwxyz123456`,
		`sk-abcdefghijklmnopqrstuv`,
	}, "\n")
	_, _ = output.Write([]byte(secretCorpus))
	for written := len(secretCorpus); written < 10<<20; {
		chunk := strings.Repeat("x", 32<<10)
		n, err := output.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("bounded writer: n=%d err=%v", n, err)
		}
		written += n
	}
	got := output.String()
	for _, secret := range []string{"quoted secret value", "space separated value", "json-secret", "bearer-secret-value", "query-secret", "ghp_abcdefghijklmnopqrstuvwxyz123456", "sk-abcdefghijklmnopqrstuv"} {
		if strings.Contains(got, secret) {
			t.Fatalf("hook output leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[hook output truncated]") {
		t.Fatalf("missing truncation marker: len=%d", len(got))
	}
	if len(got) > hookOutputLimit+len("\n[hook output truncated]") {
		t.Fatalf("bounded hook output exceeded cap: %d", len(got))
	}
}
