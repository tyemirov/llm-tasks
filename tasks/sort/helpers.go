package sort

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/tyemirov/llm-tasks/internal/config"
)

// (Kept for modularity if you want to split later. Currently, main helpers live in task.go.)
// This file can host extra helpers if needed later.
var _ = json.Marshal
var _ = os.Getenv
var _ = regexp.MustCompile
var _ = strings.TrimSpace
var _ = config.LoadSort
