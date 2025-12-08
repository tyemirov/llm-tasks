package fsops_test

import (
	"testing"

	"github.com/tyemirov/llm-tasks/internal/fsops"
)

func TestInventoryAndOps_InMemory(t *testing.T) {
	mem := fsops.NewMem()
	fs := fsops.NewOps(mem)

	// Seed directories/files in memory
	if err := mem.MkdirAll("/root/_sorted", 0o755); err != nil {
		t.Fatalf("mkdir _sorted: %v", err)
	}
	if err := mem.MkdirAll("/root/.git", 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := mem.MkdirAll("/root", 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := mem.WriteFile("/root/a.csv", []byte("x,y\n1,2\n"), 0o644); err != nil {
		t.Fatalf("write a.csv: %v", err)
	}
	if err := mem.WriteFile("/root/b.stl", []byte("solid\nendsolid\n"), 0o644); err != nil {
		t.Fatalf("write b.stl: %v", err)
	}
	if err := mem.WriteFile("/root/_sorted/ignored.csv", []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write ignored: %v", err)
	}

	files, err := fs.Inventory("/root")
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Move flow (still in-memory)
	if err := mem.WriteFile("/src.txt", []byte("hi"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := "/nested/dir/dst.txt"
	if err := fs.EnsureDir(dst); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := fs.MoveFile("/src.txt", dst); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if fs.FileExists("/src.txt") {
		t.Fatalf("src should not exist after move")
	}
	if !fs.FileExists(dst) {
		t.Fatalf("dst should exist after move")
	}
}
