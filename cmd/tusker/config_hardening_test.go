package main

import (
	"strings"
	"testing"
)

func TestHookOutputIsRedacted(t *testing.T) {
	secretCorpus := strings.Join([]string{
		`token: "quoted secret value"`,
		`password=space separated value`,
		`{"api_key":"json-secret","safe":"kept"}`,
		`Authorization: Bearer bearer-secret-value`,
		`https://example.test/?access_token=query-secret&safe=1`,
		`ghp_abcdefghijklmnopqrstuvwxyz123456`,
		`sk-abcdefghijklmnopqrstuv`,
	}, "\n")
	got := redactHookOutput(secretCorpus)
	for _, secret := range []string{"quoted secret value", "space separated value", "json-secret", "bearer-secret-value", "query-secret", "ghp_abcdefghijklmnopqrstuvwxyz123456", "sk-abcdefghijklmnopqrstuv"} {
		if strings.Contains(got, secret) {
			t.Fatalf("hook output leaked %q: %s", secret, got)
		}
	}
}
