package docgraph

import "testing"

// Every managed document except a forwarding stub must carry read_when and
// skip_when, because docs find and browse rely on them for one-glance triage.
func TestTriageFieldsRequired(t *testing.T) {
	cases := []struct {
		name, header string
		want         []string
	}{
		{"both present", "read_when: \"Changing gates.\"\nskip_when: \"Anything else.\"\n", nil},
		{"skip missing", "read_when: \"Changing gates.\"\n", []string{`missing required header field "skip_when"`}},
		{"both blank", "read_when: \"\"\nskip_when: \" \"\n", []string{`missing required header field "read_when"`, `missing required header field "skip_when"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocHeaders("docs/system/gates.md", []byte("---\nsubject: gates\npart_of: overview\nstatus: current\ndescribes: [internal/gates]\n"+tc.header+"---\n# Gates\n"))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, issue := range validateHeader(doc) {
				got = append(got, issue.Message)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("issues = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("issues = %q, want %q", got, tc.want)
				}
			}
		})
	}

	stub, err := ParseDocHeaders("docs/system/old.md", []byte("---\nstatus: superseded\nsuperseded_by: gates\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if issues := validateHeader(stub); len(issues) != 0 {
		t.Fatalf("forwarding stub must stay exempt, got %v", issues)
	}
}
