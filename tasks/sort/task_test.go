package sort_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tyemirov/llm-tasks/internal/pipeline"
	sorttask "github.com/tyemirov/llm-tasks/tasks/sort"
)

// --- helpers ---

func writeTempFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func makeTempConfig(t *testing.T, downloads, staging string, dryRun bool) string {
	t.Helper()
	cfg := `grant:
  base_directories:
    downloads: "` + downloads + `"
    staging: "` + staging + `"
  safety:
    dry_run: ` + map[bool]string{true: "true", false: "false"}[dryRun] + `
projects:
  - name: "Data_CSV"
    target: "Data_CSV"
    keywords: ["csv"]
thresholds:
  min_confidence: 0.6
`
	dir := t.TempDir()
	path := filepath.Join(dir, "task.sort.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func marshalResults(t *testing.T, results []sorttask.LLMResult) string {
	t.Helper()
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// --- tests ---

func TestSort_VerifyAndApply_DryRun(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "001")
	staging := filepath.Join(base, "001", "_sorted")
	_ = os.MkdirAll(downloads, 0o755)

	img := writeTempFile(t, downloads, "image.png", "png-bytes")
	csv := writeTempFile(t, downloads, "report.csv", "a,b,c\n1,2,3\n")

	cfgPath := makeTempConfig(t, downloads, staging, true)
	t.Setenv("LLMTASKS_SORT_CONFIG", cfgPath)

	task := sorttask.New().(*sorttask.Task)
	_, err := task.Gather(context.Background())
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	results := []sorttask.LLMResult{
		{ProjectName: "Data_CSV", TargetSubdir: "Data_CSV", Confidence: 0.90, Signals: []string{"csv ext"}},
		{ProjectName: "", TargetSubdir: "Unsorted_Inbox", Confidence: 0.70, Signals: []string{"unknown"}},
	}

	// Verify
	ok, verified, refine, err := task.Verify(
		context.Background(),
		task.Inventory,
		pipeline.LLMResponse{RawText: marshalResults(t, results)},
	)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if refine != nil {
		t.Fatalf("unexpected refine: %+v", refine)
	}
	if !ok {
		t.Fatalf("verify not accepted")
	}

	// Apply (dry run)
	report, err := task.Apply(context.Background(), verified)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !report.DryRun {
		t.Fatalf("expected dry-run")
	}
	if _, err := os.Stat(img); err != nil {
		t.Fatalf("expected image to still exist: %v", err)
	}
	if _, err := os.Stat(csv); err != nil {
		t.Fatalf("expected csv to still exist: %v", err)
	}
}

func TestSort_Verify_RejectsCountMismatch(t *testing.T) {
	base := t.TempDir()
	downloads := filepath.Join(base, "001")
	staging := filepath.Join(base, "001", "_sorted")
	_ = os.MkdirAll(downloads, 0o755)

	_ = writeTempFile(t, downloads, "a.txt", "x")
	_ = writeTempFile(t, downloads, "b.txt", "y")

	cfgPath := makeTempConfig(t, downloads, staging, true)
	t.Setenv("LLMTASKS_SORT_CONFIG", cfgPath)

	task := sorttask.New().(*sorttask.Task)
	if _, err := task.Gather(context.Background()); err != nil {
		t.Fatalf("gather: %v", err)
	}

	// LLM returns only 1 item for 2 files -> should request refine
	resp := marshalResults(t, []sorttask.LLMResult{
		{ProjectName: "", TargetSubdir: "Unsorted_Inbox", Confidence: 1.0},
	})
	ok, _, refine, err := task.Verify(
		context.Background(),
		task.Inventory,
		pipeline.LLMResponse{RawText: resp},
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatalf("expected not accepted due to count mismatch")
	}
	if refine == nil || refine.Reason != "count-mismatch" {
		t.Fatalf("expected refine: count-mismatch, got %+v", refine)
	}
}
