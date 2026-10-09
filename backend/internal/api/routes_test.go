package api

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRoutesMatchOpenAPI keeps the Go route table and docs/openapi.yaml in sync.
func TestRoutesMatchOpenAPI(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	inSpec := map[string]bool{}
	for p, ops := range spec.Paths {
		for m := range ops {
			switch m {
			case "get", "post", "put", "delete", "patch":
				inSpec[strings.ToUpper(m)+" "+p] = true
			}
		}
	}
	inGo := map[string]bool{}
	for _, r := range Routes {
		inGo[r] = true
	}
	var missing []string
	for r := range inSpec {
		if !inGo[r] {
			missing = append(missing, "not in Routes: "+r)
		}
	}
	for r := range inGo {
		if !inSpec[r] {
			missing = append(missing, "not in openapi.yaml: "+r)
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Error(m)
	}
}
