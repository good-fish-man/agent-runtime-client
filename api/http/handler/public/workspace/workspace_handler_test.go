package workspace

import (
	"os"
	"strings"
	"testing"
)

func TestContextExcerptCentersRelevantContent(t *testing.T) {
	content := strings.Repeat("before line\n", 200) +
		"func generatePatch() { return relevantChange }\n" +
		strings.Repeat("after line\n", 200)

	excerpt := contextExcerpt(content, []string{"generatepatch"}, 512)
	if len(excerpt) > 512 {
		t.Fatalf("excerpt length = %d, want <= 512", len(excerpt))
	}
	if !strings.Contains(excerpt, "func generatePatch") {
		t.Fatal("excerpt does not include the matching content")
	}
	_, startLine := contextExcerptWithLine(content, []string{"generatepatch"}, 512)
	if startLine <= 1 {
		t.Fatalf("start line = %d, want an offset excerpt", startLine)
	}
}

func TestContextExcerptKeepsSmallFile(t *testing.T) {
	const content = "small file\n"
	if got := contextExcerpt(content, []string{"small"}, 512); got != content {
		t.Fatalf("contextExcerpt() = %q, want %q", got, content)
	}
}

func TestPreferNamedContextFiles(t *testing.T) {
	candidates := []contextFile{
		{Path: "README.md", Score: 10},
		{Path: "src/ProjectWorkspace.tsx", Score: 9},
	}
	got := preferNamedContextFiles(candidates, "请修改 README.md 的结尾")
	if len(got) != 1 || got[0].Path != "README.md" {
		t.Fatalf("preferNamedContextFiles() = %#v", got)
	}
	if got := preferNamedContextFiles(candidates, "优化整个项目"); len(got) != len(candidates) {
		t.Fatalf("broad query should retain all candidates, got %#v", got)
	}
}

func TestNormalizePatchPreservesFinalNewline(t *testing.T) {
	patch := "```diff\ndiff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n```"
	got := normalizePatch(patch)
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("normalized patch must end with a newline: %q", got)
	}
	if strings.Contains(got, "```") {
		t.Fatalf("normalized patch still contains a markdown fence: %q", got)
	}
	root := t.TempDir()
	if err := os.WriteFile(root+"/a.txt", []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := gitApply(root, got, true); err != nil {
		t.Fatalf("normalized patch failed git apply check: %v", err)
	}
}

func TestBuildWorkspacePatchProducesApplicableDiff(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/README.md", []byte("first\nlast\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := buildWorkspacePatch(root, []patchChange{{
		Path:    "README.md",
		Find:    "last\n",
		Replace: "last\nadded\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+added") {
		t.Fatalf("generated patch does not contain addition: %s", patch)
	}
	if err := gitApply(root, patch, true); err != nil {
		t.Fatalf("generated patch failed git apply check: %v\n%s", err, patch)
	}
}

func TestFindMatchLines(t *testing.T) {
	content := "target\nkeep\ntarget\nkeep\ntarget\n"
	got := findMatchLines(content, "target", 8)
	want := []int{1, 3, 5}
	if len(got) != len(want) {
		t.Fatalf("findMatchLines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findMatchLines() = %v, want %v", got, want)
		}
	}
}

func TestBuildWorkspacePatchUsesOccurrence(t *testing.T) {
	root := t.TempDir()
	content := "target\none\ntarget\ntwo\ntarget\nthree\ntarget\nfour\n"
	if err := os.WriteFile(root+"/main.go", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := buildWorkspacePatch(root, []patchChange{{
		Path:       "main.go",
		Find:       "target",
		Replace:    "selected",
		Occurrence: 4,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := gitApply(root, patch, false); err != nil {
		t.Fatalf("apply occurrence patch: %v\n%s", err, patch)
	}
	data, err := os.ReadFile(root + "/main.go")
	if err != nil {
		t.Fatal(err)
	}
	want := "target\none\ntarget\ntwo\ntarget\nthree\nselected\nfour\n"
	if string(data) != want {
		t.Fatalf("updated content = %q, want %q", data, want)
	}
}

func TestBuildWorkspacePatchUsesLineRanges(t *testing.T) {
	root := t.TempDir()
	content := "first\nsecond\nthird\nfourth\n"
	if err := os.WriteFile(root+"/main.go", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := buildWorkspacePatch(root, []patchChange{
		{Path: "main.go", StartLine: 2, EndLine: 2, Replace: "SECOND\ninserted"},
		{Path: "main.go", StartLine: 4, EndLine: 4, Replace: "FOURTH"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gitApply(root, patch, false); err != nil {
		t.Fatalf("apply line range patch: %v\n%s", err, patch)
	}
	data, err := os.ReadFile(root + "/main.go")
	if err != nil {
		t.Fatal(err)
	}
	want := "first\nSECOND\ninserted\nthird\nFOURTH\n"
	if string(data) != want {
		t.Fatalf("updated content = %q, want %q", data, want)
	}
}

func TestBuildWorkspacePatchAppendsAfterEOF(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/main.go", []byte("first\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := buildWorkspacePatch(root, []patchChange{{
		Path:      "main.go",
		StartLine: 3,
		EndLine:   20,
		Replace:   "third\nfourth",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := gitApply(root, patch, false); err != nil {
		t.Fatalf("apply EOF append patch: %v\n%s", err, patch)
	}
	data, err := os.ReadFile(root + "/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "first\nsecond\nthird\nfourth\n"; got != want {
		t.Fatalf("updated content = %q, want %q", got, want)
	}
}

func TestTextLineCount(t *testing.T) {
	for content, want := range map[string]int{"": 0, "one": 1, "one\n": 1, "one\ntwo\n": 2} {
		if got := textLineCount(content); got != want {
			t.Fatalf("textLineCount(%q) = %d, want %d", content, got, want)
		}
	}
}
