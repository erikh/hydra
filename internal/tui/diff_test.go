package tui

import (
	"strings"
	"testing"
)

func TestComputeUnifiedDiffIdentical(t *testing.T) {
	diff := ComputeUnifiedDiff("file.go", "hello\nworld\n", "hello\nworld\n")
	if !strings.HasPrefix(diff, "--- a/file.go\n+++ b/file.go\n") {
		t.Errorf("missing header:\n%s", diff)
	}
	if strings.Contains(diff, "+") && !strings.Contains(diff, "+++") {
		t.Error("identical content should have no additions")
	}
}

func TestComputeUnifiedDiffAddLine(t *testing.T) {
	diff := ComputeUnifiedDiff("f.go", "a\nc\n", "a\nb\nc\n")
	if !strings.Contains(diff, "+b") {
		t.Errorf("expected added line +b:\n%s", diff)
	}
}

func TestComputeUnifiedDiffRemoveLine(t *testing.T) {
	diff := ComputeUnifiedDiff("f.go", "a\nb\nc\n", "a\nc\n")
	if !strings.Contains(diff, "-b") {
		t.Errorf("expected removed line -b:\n%s", diff)
	}
}

func TestComputeUnifiedDiffReplaceLine(t *testing.T) {
	diff := ComputeUnifiedDiff("f.go", "old line\n", "new line\n")
	if !strings.Contains(diff, "-old line") {
		t.Errorf("expected removed old line:\n%s", diff)
	}
	if !strings.Contains(diff, "+new line") {
		t.Errorf("expected added new line:\n%s", diff)
	}
}

func TestComputeUnifiedDiffEmptyOld(t *testing.T) {
	diff := ComputeUnifiedDiff("new.go", "", "package main\n")
	if !strings.Contains(diff, "+package main") {
		t.Errorf("expected all lines added:\n%s", diff)
	}
}

func TestComputeUnifiedDiffEmptyNew(t *testing.T) {
	diff := ComputeUnifiedDiff("del.go", "package main\n", "")
	if !strings.Contains(diff, "-package main") {
		t.Errorf("expected all lines removed:\n%s", diff)
	}
}

func TestComputeUnifiedDiffHeader(t *testing.T) {
	diff := ComputeUnifiedDiff("src/main.go", "a\n", "b\n")
	if !strings.HasPrefix(diff, "--- a/src/main.go\n+++ b/src/main.go\n") {
		t.Errorf("header should include path:\n%s", diff)
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	result := RenderDiff("", DefaultTheme())
	if result != "" {
		t.Errorf("empty diff should render empty, got %q", result)
	}
}

func TestRenderDiffColorizesLines(t *testing.T) {
	diff := "--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-old\n+new\n context\n"
	rendered := RenderDiff(diff, DefaultTheme())

	// Rendered output should contain all the original text (surrounded by ANSI codes).
	for _, text := range []string{"--- a/f.go", "+++ b/f.go", "@@ -1 +1 @@", "-old", "+new", " context"} {
		if !strings.Contains(rendered, text) {
			t.Errorf("rendered diff missing %q:\n%s", text, rendered)
		}
	}
}
