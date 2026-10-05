package docgraph

import "testing"

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"cmd/tusker/*.go", "cmd/tusker/cli.go", true},
		{"cmd/tusker/*.go", "cmd/tusker/sub/x.go", false},
		{"internal/**/foo.go", "internal/foo.go", true},
		{"internal/**/foo.go", "internal/a/b/foo.go", true},
		{"internal/**", "internal/a/b.go", true},
		{"internal/**", "other/a.go", false},
		{"a/[bc]?.go", "a/cx.go", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestDocTouchMatchesGlobDescribes(t *testing.T) {
	corpus := Corpus{Documents: []Document{
		{Path: "docs/system/cli.md", Kind: KindCanonical, Subject: "cli", Describes: []string{"cmd/tusker/*.go"}},
		{Path: "docs/system/deep.md", Kind: KindCanonical, Subject: "deep", Describes: []string{"internal/**/foo.go"}},
		{Path: "docs/system/other.md", Kind: KindCanonical, Subject: "other", Describes: []string{"pkg/*.go"}},
	}}
	report := CheckDocTouch(corpus, []string{"cmd/tusker/cli.go", "internal/a/b/foo.go"}, nil)
	if len(report.Missing) != 2 || report.Missing[0].Subject != "cli" || report.Missing[1].Subject != "deep" {
		t.Fatalf("unexpected doc-touch report: %#v", report.Missing)
	}
}

func TestGlobCoversDirectoryGlobs(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"src*/", "src-a/main.go", true},
		{"internal/*", "internal/a/b.go", true},
		{"src*/", "lib/main.go", false},
	}
	for _, c := range cases {
		if got := globCovers(c.pattern, c.name); got != c.want {
			t.Errorf("globCovers(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
	corpus := Corpus{Documents: []Document{{Path: "docs/system/src.md", Kind: KindCanonical, Subject: "src", Describes: []string{"src*/"}}}}
	if report := CheckDocTouch(corpus, []string{"src-a/main.go"}, nil); len(report.Missing) != 1 {
		t.Fatalf("trailing-slash glob describes missed a change: %#v", report.Missing)
	}
}
