package changelog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tyemirov/llm-tasks/internal/config"
	"github.com/tyemirov/llm-tasks/internal/pipeline"
)

// Make the task's Config exactly the same type as config.ChangelogConfig.
type Config = config.ChangelogConfig

type Task struct {
	cfg     Config
	version string
	date    string
	gitLog  string
	request pipeline.LLMRequest
	section string
}

// New provides a zero-arg factory for CLI registry.
// It reads LLMTASKS_CHANGELOG_CONFIG or defaults to configs/task.changelog.yaml.
// If the YAML cannot be loaded, it returns a failTask that surfaces the error on Gather.
func New() pipeline.Pipeline {
	path := strings.TrimSpace(os.Getenv("LLMTASKS_CHANGELOG_CONFIG"))
	if path == "" {
		path = "configs/task.changelog.yaml"
	}
	t, err := NewFromYAML(path)
	if err != nil {
		return failTask{err: fmt.Errorf("changelog config load (%s): %w", path, err)}
	}
	return t
}

func NewFromYAML(yamlPath string) (*Task, error) {
	data, err := os.ReadFile(filepath.Clean(yamlPath))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Task == "" {
		return nil, errors.New("configs/task.changelog.yaml: task field is required")
	}
	return &Task{cfg: cfg}, nil
}

func (t *Task) Name() string { return "changelog" }

// 1) Gather: version, date, git log (stdin)
func (t *Task) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	v := coalesce(os.Getenv(t.cfg.Inputs.Version.Env), t.cfg.Inputs.Version.Default)
	d := coalesce(os.Getenv(t.cfg.Inputs.Date.Env), t.cfg.Inputs.Date.Default)

	if t.cfg.Inputs.Version.Required && strings.TrimSpace(v) == "" {
		return nil, errors.New("version is required (pass --version or set env)")
	}
	if t.cfg.Inputs.Date.Required && strings.TrimSpace(d) == "" {
		return nil, errors.New("date is required (pass --date or set env)")
	}

	var gl string
	if strings.EqualFold(t.cfg.Inputs.GitLog.Source, "stdin") {
		var buf bytes.Buffer
		if err := readAllToBufferCtx(ctx, os.Stdin, &buf); err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		gl = strings.TrimSpace(buf.String())
	}
	if t.cfg.Inputs.GitLog.Required && gl == "" {
		return nil, errors.New("git_log is required on stdin")
	}

	t.version, t.date, t.gitLog = v, d, gl
	return map[string]string{"version": v, "date": d, "git_log": gl}, nil
}

// 2) Prompt: build system+user prompts from YAML
func (t *Task) Prompt(ctx context.Context, _ pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	sys := strings.TrimSpace(t.cfg.Recipe.System)
	heading := expandTemplate(t.cfg.Recipe.Format.Heading, map[string]string{
		"version": t.version,
		"date":    t.date,
	})

	var sb strings.Builder
	sb.WriteString("Summarize the following git log into a Markdown changelog section.\n\n")
	sb.WriteString("Format:\n")
	sb.WriteString(heading)
	sb.WriteString("\n\n")
	for _, s := range t.cfg.Recipe.Format.Sections {
		sb.WriteString("### ")
		sb.WriteString(s.Title)
		sb.WriteString("\n\n")
	}
	if foot := strings.TrimSpace(t.cfg.Recipe.Format.Footer); foot != "" {
		sb.WriteString(foot)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Rules:\n")
	for _, r := range t.cfg.Recipe.Rules {
		sb.WriteString("- ")
		sb.WriteString(r)
		sb.WriteString("\n")
	}
	sb.WriteString("\nGit log:\n")
	sb.WriteString(t.gitLog)

	t.request = pipeline.LLMRequest{
		SystemPrompt: sys,
		UserPrompt:   sb.String(),
		MaxTokens:    max1(t.cfg.LLM.MaxTokens, 1200),
		Temperature:  t.cfg.LLM.Temperature,
		Model:        t.cfg.LLM.Model,
	}
	return t.request, nil
}

// 3) Verify
func (t *Task) Verify(ctx context.Context, _ pipeline.GatherOutput, response pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	md := strings.TrimSpace(response.RawText)

	// No code fences
	if strings.Contains(md, "```") {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: "Do not use code fences. Return plain Markdown only.",
			Reason:          "code-fences",
		}, nil
	}

	// Must start with expected heading
	wantPrefix := expandTemplate(t.cfg.Recipe.Format.Heading, map[string]string{
		"version": t.version,
		"date":    t.date,
	})
	if !strings.HasPrefix(md, strings.TrimSpace(wantPrefix)) {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("Your output must start with the exact heading: %q", strings.TrimSpace(wantPrefix)),
			Reason:          "bad-heading",
		}, nil
	}

	// Ensure each configured section title exists
	for _, s := range t.cfg.Recipe.Format.Sections {
		needle := "### " + s.Title
		if !strings.Contains(md, needle) {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: fmt.Sprintf("Include the section heading %q exactly, even if empty.", needle),
				Reason:          "missing-section",
			}, nil
		}
	}

	// Highlights min constraint (if > 0)
	if len(t.cfg.Recipe.Format.Sections) > 0 && t.cfg.Recipe.Format.Sections[0].Title == "Highlights" && t.cfg.Recipe.Format.Sections[0].Min > 0 {
		highlightsBlock := extractSection(md, "Highlights")
		bullets := countBullets(highlightsBlock)
		if bullets < t.cfg.Recipe.Format.Sections[0].Min {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: fmt.Sprintf("Provide at least %d concise bullets under 'Highlights'.", t.cfg.Recipe.Format.Sections[0].Min),
				Reason:          "too-few-highlights",
			}, nil
		}
	}

	t.section = md
	return true, md, nil, nil
}

// 4) Apply: prepend to CHANGELOG.md or print
func (t *Task) Apply(ctx context.Context, verified pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	md := verified.(string)
	switch strings.ToLower(t.cfg.Apply.Mode) {
	case "print":
		fmt.Println(md)
		return pipeline.ApplyReport{DryRun: false, Summary: "printed changelog section", NumActions: 1}, nil
	case "prepend":
		path := coalesce(t.cfg.Apply.OutputPath, "./CHANGELOG.md")
		var existing string
		if b, err := os.ReadFile(filepath.Clean(path)); err == nil {
			existing = string(b)
		}
		var out strings.Builder
		out.WriteString(md)
		out.WriteString("\n")
		if t.cfg.Apply.EnsureBlankLine {
			out.WriteString("\n")
		}
		out.WriteString(strings.TrimLeft(existing, "\n"))
		if err := os.WriteFile(filepath.Clean(path), []byte(out.String()), 0o644); err != nil {
			return pipeline.ApplyReport{}, err
		}
		return pipeline.ApplyReport{DryRun: false, Summary: "prepended changelog to " + path, NumActions: 1}, nil
	default:
		return pipeline.ApplyReport{}, fmt.Errorf("unknown apply.mode: %s", t.cfg.Apply.Mode)
	}
}

// --- helpers ---

func readAllToBufferCtx(ctx context.Context, r io.Reader, dst *bytes.Buffer) error {
	sc := bufio.NewScanner(r)
	done := make(chan error, 1)
	go func() {
		for sc.Scan() {
			_, _ = dst.Write(sc.Bytes())
			_, _ = dst.WriteString("\n")
		}
		done <- sc.Err()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func extractSection(md, title string) string {
	start := "### " + title
	i := strings.Index(md, start)
	if i < 0 {
		return ""
	}
	rest := md[i+len(start):]
	// Stop at next "### " or end
	j := strings.Index(rest, "\n### ")
	if j >= 0 {
		return rest[:j]
	}
	return rest
}

func countBullets(s string) int {
	c := 0
	for _, line := range strings.Split(s, "\n") {
		lt := strings.TrimSpace(line)
		if strings.HasPrefix(lt, "- ") || strings.HasPrefix(lt, "* ") {
			c++
		}
	}
	return c
}

func expandTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
	}
	return out
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func max1(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

// failTask surfaces configuration load errors through the normal Runner flow.
type failTask struct{ err error }

func (f failTask) Name() string { return "changelog" }
func (f failTask) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	return nil, f.err
}
func (f failTask) Prompt(ctx context.Context, _ pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	return pipeline.LLMRequest{}, f.err
}
func (f failTask) Verify(ctx context.Context, _ pipeline.GatherOutput, _ pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	return false, nil, nil, f.err
}
func (f failTask) Apply(ctx context.Context, _ pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	return pipeline.ApplyReport{}, f.err
}

// NewFromConfig constructs the task directly from an already-parsed config.
func NewFromConfig(cfg Config) *Task {
	return &Task{cfg: cfg}
}
