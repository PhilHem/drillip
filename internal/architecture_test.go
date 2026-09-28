package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Check production imports directly: ch's Go graph does not yet resolve every
// package import. Integration tests may wire concrete adapters together.
func TestArchitectureImports(t *testing.T) {
	const module = "github.com/PhilHem/drillip/internal/"
	rules := map[string][]string{
		"domain/":               {"domain/"},
		"application/port/in/":  {"domain/", "application/port/in/"},
		"application/port/out/": {"domain/", "application/port/out/"},
		"application/service/":  {"domain/", "application/port/in/", "application/port/out/", "application/service/"},
		"adapter/in/":           {"domain/", "application/port/in/"},
		"adapter/out/":          {"domain/", "application/port/out/"},
		"bootstrap/":            {""},
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		path = filepath.ToSlash(path)
		var allowed []string
		for prefix, dependencies := range rules {
			if strings.HasPrefix(path, prefix) {
				allowed = dependencies
				break
			}
		}
		if allowed == nil {
			t.Errorf("%s has no architecture rule", path)
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(dependency, module) {
				continue
			}
			target := strings.TrimPrefix(dependency, module) + "/"
			permitted := false
			for _, prefix := range allowed {
				if strings.HasPrefix(target, prefix) {
					permitted = true
					break
				}
			}
			if !permitted {
				t.Errorf("%s imports %s across an architecture boundary", path, dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
