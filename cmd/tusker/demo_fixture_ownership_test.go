package main

import (
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDemoFixtureOwnsContractFiles(t *testing.T) {
	owners := map[string]string{}
	total := 0
	for _, wave := range demoFixtureWaves() {
		file, err := demoRenderWaveAuthoring(t.TempDir(), wave, false)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var request struct {
			Tasks []struct {
				Key              string
				Body             string
				OwnedPaths       []string `yaml:"owned_paths"`
				GeneratedOutputs []string `yaml:"generated_outputs"`
			}
		}
		if err := yaml.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		if len(request.Tasks) != len(wave.Tasks) {
			t.Fatalf("wave %s lost tasks during rendering", wave.Name)
		}
		for i, task := range wave.Tasks {
			total++
			seeded := request.Tasks[i]
			stem := strings.ReplaceAll(strings.TrimSuffix(path.Base(task.Artifact), ".txt"), "-", "_")
			want := []string{task.Artifact, path.Join(path.Dir(task.Artifact), stem+".py"), path.Join(path.Dir(task.Artifact), "test_"+stem+".py")}
			if seeded.Key != task.Key || !reflect.DeepEqual(seeded.OwnedPaths, want) {
				t.Fatalf("task %s seeded ownership = %v, want %v", task.Key, seeded.OwnedPaths, want)
			}
			if !strings.Contains(seeded.Body, "Owned paths: "+strings.Join(want, ", ")) {
				t.Fatalf("task %s body disagrees with seeded ownership", task.Key)
			}
			for _, owned := range seeded.OwnedPaths {
				if previous, exists := owners[owned]; exists {
					t.Fatalf("tasks %s and %s share %s", previous, task.Key, owned)
				}
				owners[owned] = task.Key
			}
			if !reflect.DeepEqual(seeded.GeneratedOutputs, []string{task.Artifact}) {
				t.Fatalf("task %s changed offline outputs: %v", task.Key, seeded.GeneratedOutputs)
			}
			check := `test "$(cat ` + task.Artifact + `)" = "` + task.Content + `"`
			if !strings.Contains(seeded.Body, "Exact verification (offline): "+check) {
				t.Fatalf("task %s changed offline verification", task.Key)
			}
		}
	}
	if total != 13 || len(owners) != 39 {
		t.Fatalf("got %d tasks owning %d files, want 13 tasks and 39 files", total, len(owners))
	}
}
