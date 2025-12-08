package sort

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/tyemirov/llm-tasks/internal/config"
	"github.com/tyemirov/llm-tasks/internal/fsops"
	"github.com/tyemirov/llm-tasks/internal/pipeline"
)

type Task struct {
	fs      fsops.Ops
	cfgProv SortConfigProvider

	Inventory []FileMeta
	Plan      MovePlan
}

func New() pipeline.Pipeline {
	return NewWithDeps(
		fsops.NewOps(fsops.NewOS()),
		FileSortConfigProvider{PathEnv: "LLMTASKS_SORT_CONFIG", DefaultPath: "configs/task.sort.yaml"},
	)
}

func NewWithDeps(fs fsops.Ops, cfg SortConfigProvider) pipeline.Pipeline {
	return &Task{fs: fs, cfgProv: cfg}
}

// DefaultFS exported for wiring from the runner
func DefaultFS() fsops.Ops { return fsops.NewOps(fsops.NewOS()) }

type FileMeta struct {
	AbsolutePath string `json:"absolute_path"`
	BaseName     string `json:"base_name"`
	Extension    string `json:"extension"`
	MIMEType     string `json:"mime"`
	SizeBytes    int64  `json:"size_bytes"`
}

type LLMResult struct {
	ProjectName      string   `json:"project_name"`
	TargetSubdir     string   `json:"target_subdir"`
	Confidence       float64  `json:"confidence"`
	IsNewProject     bool     `json:"is_new_project"`
	ProposedProject  string   `json:"proposed_project"`
	ProposedKeywords []string `json:"proposed_keywords"`
	Signals          []string `json:"signals"`
}

type MoveAction struct {
	FromPath   string  `json:"from"`
	ToPath     string  `json:"to"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type MovePlan struct {
	Actions []MoveAction `json:"actions"`
	DryRun  bool         `json:"dry_run"`
}

func (t *Task) Name() string { return "sort" }

// 1) Gather
func (t *Task) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	cfg, err := t.cfgProv.Load()
	if err != nil {
		return nil, err
	}
	infos, err := t.fs.Inventory(cfg.Grant.BaseDirectories.Downloads)
	if err != nil {
		return nil, err
	}
	result := make([]FileMeta, 0, len(infos))
	for _, info := range infos {
		result = append(result, FileMeta{
			AbsolutePath: info.AbsolutePath,
			BaseName:     info.BaseName,
			Extension:    info.Extension,
			MIMEType:     info.MIMEType,
			SizeBytes:    info.SizeBytes,
		})
	}
	t.Inventory = result
	return result, nil
}

// 2) Prompt
func (t *Task) Prompt(ctx context.Context, gathered pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	files := gathered.([]FileMeta)
	filesJSON, _ := json.Marshal(files)

	system := strings.TrimSpace(`
You classify files into project folders using only the provided metadata.
- Return JSON array, one element per input in the same order.
- If no project fits, propose a concise new project and keywords.
- Confidence 0..1. No prose. No code fences.
`)

	user := fmt.Sprintf(`Existing projects:
%s

File metadata (array):
%s

Respond as JSON array with objects:
{"project_name":"","target_subdir":"","confidence":0.0,"is_new_project":false,"proposed_project":"","proposed_keywords":[],"signals":[]}
`, t.loadProjectListJSON(), string(filesJSON))

	return pipeline.LLMRequest{
		SystemPrompt: system,
		UserPrompt:   user,
		MaxTokens:    1200,
		Temperature:  0.1,
	}, nil
}

// 3) Verify (+ optional refine)
func (t *Task) Verify(ctx context.Context, gathered pipeline.GatherOutput, response pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	var parsed []LLMResult
	if err := json.Unmarshal([]byte(response.RawText), &parsed); err != nil {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: "The previous output was not valid JSON. Re-send strictly valid JSON only.",
			Reason:          "invalid-json",
		}, nil
	}
	files := gathered.([]FileMeta)
	if len(parsed) != len(files) {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("You returned %d items for %d files. Return exactly one classification per file, ordered.", len(parsed), len(files)),
			Reason:          "count-mismatch",
		}, nil
	}

	cfg, err := t.cfgProv.Load()
	if err != nil {
		return false, nil, nil, err
	}
	minConfidence := cfg.Thresholds.MinConfidence
	if minConfidence <= 0 {
		minConfidence = 0.6
	}
	projectNamePattern := regexp.MustCompile(`^[\w\- ]{2,64}$`)

	var actions []MoveAction
	for idx, item := range parsed {
		if item.TargetSubdir == "" {
			item.TargetSubdir = "Unsorted_Inbox"
		}
		if item.IsNewProject {
			if !projectNamePattern.MatchString(item.ProposedProject) {
				return false, nil, &pipeline.RefineRequest{
					UserPromptDelta: "The proposed project name is invalid. Use 2–64 characters: letters, numbers, space, dash, underscore.",
					Reason:          "bad-project-name",
				}, nil
			}
		}
		if item.Confidence < minConfidence && !item.IsNewProject {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: "For low-confidence items, either raise confidence with clearer signals or assign 'Unsorted_Inbox'. Confidence must be >= threshold unless is_new_project=true.",
				Reason:          "low-confidence",
			}, nil
		}
		targetDir := safeSegment(item.TargetSubdir)
		to := t.fs.FS.Join(cfg.Grant.BaseDirectories.Staging, targetDir, files[idx].BaseName+files[idx].Extension)
		actions = append(actions, MoveAction{
			FromPath:   files[idx].AbsolutePath,
			ToPath:     to,
			Confidence: item.Confidence,
			Reason:     strings.Join(item.Signals, ","),
		})
	}
	plan := MovePlan{Actions: actions, DryRun: cfg.Grant.Safety.DryRun}
	t.Plan = plan
	return true, plan, nil, nil
}

// 4) Apply
func (t *Task) Apply(ctx context.Context, verified pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	plan := verified.(MovePlan)
	return t.applyMovePlan(plan)
}

// --- local helpers ---

func (t *Task) loadProjectListJSON() string {
	cfg, _ := t.cfgProv.Load()
	type Project struct {
		Name     string   `json:"name"`
		Target   string   `json:"target"`
		Keywords []string `json:"keywords"`
	}
	var arr []Project
	for _, p := range cfg.Projects {
		arr = append(arr, Project{Name: p.Name, Target: p.Target, Keywords: p.Keywords})
	}
	b, _ := json.Marshal(arr)
	return string(b)
}

func safeSegment(s string) string {
	s = strings.TrimSpace(s)
	re := regexp.MustCompile(`[^a-zA-Z0-9 _\-]`)
	s = re.ReplaceAllString(s, "_")
	s = strings.Trim(s, " _-")
	if s == "" {
		return "Unsorted_Inbox"
	}
	return s
}

// --- Config provider abstraction ---

type SortConfigProvider interface {
	Load() (config.Sort, error)
}

type FileSortConfigProvider struct {
	PathEnv     string
	DefaultPath string
}

func (p FileSortConfigProvider) Load() (config.Sort, error) {
	// The concrete CLI wires env / default path by constructing this provider.
	path := p.DefaultPath
	if v := strings.TrimSpace(osGetenv(p.PathEnv)); v != "" {
		path = v
	}
	sortConfiguration, loadError := config.LoadSort(path)
	if loadError != nil {
		return config.Sort{}, loadError
	}
	return resolveSortGrantBaseDirectories(sortConfiguration, lookupEnvironmentVariable)
}

// indirection to allow stubbing in rare cases (not used currently)
var osGetenv = os.Getenv
