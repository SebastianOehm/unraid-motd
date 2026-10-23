package datasources

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListComposeProjects(t *testing.T) {
	root := t.TempDir()

	stacks := []string{
		"alpha/compose.yaml",
		"beta/docker-compose.yml",
		"delta/compose.yml",
		"epsilon/docker-compose.yaml",
		"gamma/readme.txt",
	}
	for _, stack := range stacks {
		dir := filepath.Join(root, filepath.Dir(stack))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		writeTempFile(t, dir, filepath.Base(stack), "services: {}\n")
	}
	writeTempFile(t, filepath.Join(root, "beta"), composeProjectNameFile, "  beta-renamed\n")
	writeTempFile(t, root, "notes.txt", "not a stack\n")

	want := []composeProject{
		{Name: "alpha", ProjectName: "alpha"},
		{Name: "beta", ProjectName: "beta-renamed"},
		{Name: "delta", ProjectName: "delta"},
		{Name: "epsilon", ProjectName: "epsilon"},
	}

	got, err := listComposeProjects(root)
	if err != nil {
		t.Fatalf("listComposeProjects() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listComposeProjects() = %+v, want %+v", got, want)
	}

	t.Run("missing root", func(t *testing.T) {
		got, err := listComposeProjects(filepath.Join(root, "does-not-exist"))
		if err == nil {
			t.Error("listComposeProjects() error = nil, want error")
		}
		if got != nil {
			t.Errorf("listComposeProjects() = %+v, want nil", got)
		}
	})
}

func TestEffectiveProjectName(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		want    string
	}{
		{name: "plain name", content: strPtr("custom"), want: "custom"},
		{name: "surrounding whitespace is trimmed", content: strPtr(" \t custom-name \r\n"), want: "custom-name"},
		{name: "empty file", content: strPtr(""), want: "stack"},
		{name: "whitespace only", content: strPtr("  \n\t\n"), want: "stack"},
		{name: "missing file", content: nil, want: "stack"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			if testCase.content != nil {
				writeTempFile(t, dir, composeProjectNameFile, *testCase.content)
			}

			if got := effectiveProjectName(dir, "stack"); got != testCase.want {
				t.Errorf("effectiveProjectName() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// strPtr returns a pointer to s, to tell an absent value from an empty one.
func strPtr(s string) *string { return &s }
