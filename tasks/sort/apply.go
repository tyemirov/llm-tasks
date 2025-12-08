package sort

import (
	"fmt"
	"path/filepath"

	"github.com/tyemirov/llm-tasks/internal/pipeline"
)

func (t *Task) applyMovePlan(plan MovePlan) (pipeline.ApplyReport, error) {
	count := 0
	for _, a := range plan.Actions {
		if plan.DryRun {
			fmt.Printf("[DRY] %s -> %s (%.2f) %s\n", a.FromPath, a.ToPath, a.Confidence, a.Reason)
			count++
			continue
		}
		if err := t.fs.EnsureDir(a.ToPath); err != nil {
			return pipeline.ApplyReport{}, err
		}
		dest := t.uniquePath(a.ToPath)
		if err := t.fs.MoveFile(a.FromPath, dest); err != nil {
			return pipeline.ApplyReport{}, err
		}
		fmt.Printf("[MOVE] %s -> %s (%.2f)\n", a.FromPath, dest, a.Confidence)
		count++
	}
	return pipeline.ApplyReport{
		DryRun:     plan.DryRun,
		Summary:    fmt.Sprintf("sort: %d actions (%s)", count, ternary(plan.DryRun, "dry-run", "applied")),
		NumActions: count,
	}, nil
}

func (t *Task) uniquePath(to string) string {
	base := to
	ext := filepath.Ext(to)
	stem := base[:len(base)-len(ext)]
	i := 1
	for t.fs.FileExists(base) {
		base = fmt.Sprintf("%s-%d%s", stem, i, ext)
		i++
	}
	return base
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
