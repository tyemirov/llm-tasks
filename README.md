# LLM Tasks

Run structured tasks with LLMs from the command line.
Tasks are declared in `config.yaml` and executed through a simple CLI.

## Installation

```bash
git clone https://github.com/tyemirov/llm-tasks.git
cd llm-tasks
go build -o llm-tasks ./cmd/llm-tasks
```

## Configuration

All configuration lives in a single root-level file `config.yaml`.

```yaml
common:
  log_level: info
  log_format: structured
  api:
    endpoint: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  defaults:
    attempts: 3
    timeout_seconds: 45

models:
  - name: gpt-5-mini
    provider: openai
    model_id: gpt-5-mini
    default: true
    supports_temperature: false
    default_temperature: 1
    max_completion_tokens: 1500

recipes:
  - name: sort
    enabled: true
    type: task/sort
    model: gpt-5-mini
    # task-specific keys omitted for brevity
  - name: changelog
    enabled: true
    type: task/changelog
    model: gpt-5-mini
    # task-specific keys omitted for brevity
```

### Sections

* **common**
  Global settings: logging, API endpoint, API key environment variable, and default retry/timeout values.

* **models**
  Declares available models. Each entry specifies provider, model ID, whether it is the default, token limits, and
  temperature support.

* **recipes**
  Array of enabled tasks. Each recipe binds to a model and type (`task/sort`, `task/changelog`, …). Disabled recipes are
  ignored unless explicitly listed with `--all`.

### Embedded defaults

When no user configuration file is found, the embedded fallback remains active. Operators must provide the following
environment variables so the sort recipe can resolve directories safely:

* `SORT_DOWNLOADS_DIR` — absolute path to the directory containing inbound files that require sorting.
* `SORT_STAGING_DIR` — absolute path to the directory where categorized files are placed.

If either environment variable is missing, the sort recipe should be disabled in a custom configuration or the
variables should be exported before invoking the CLI.

## Usage

List registered tasks (enabled by default):

```bash
./llm-tasks list --config ./config.yaml
```

Example output:

```
sort        (enabled, model=gpt-5-mini)
changelog   (enabled, model=gpt-5-mini)
```

Show disabled recipes as well:

```bash
./llm-tasks list --config ./config.yaml --all
```

### Run a task

Run any recipe directly by name:

```bash
./llm-tasks run changelog --config ./config.yaml
./llm-tasks run sort --config ./config.yaml
```

Optional flags:

* `--model` override recipe’s model by name
* `--attempts` max refine attempts (default from config)
* `--timeout` per-attempt timeout
* `--version` changelog release version (exports to `CHANGELOG_VERSION`)
* `--date` changelog release date (exports to `CHANGELOG_DATE`)
* `--dry` dry-run mode (for tasks that support it)

### Example: changelog

Summarize recent commits into release notes:

```bash
git log --oneline --no-merges HEAD~20..HEAD > /tmp/git.log

./llm-tasks run changelog \
  --config ./config.yaml \
  --version v0.1.0 \
  --date 2025-09-27 \
  < /tmp/git.log
```

The `--version` and `--date` flags export their values to the `CHANGELOG_VERSION` and `CHANGELOG_DATE` environment variables so the changelog pipeline receives release metadata automatically.

### Example: sort

Organize files into project-based subfolders:

```bash
./llm-tasks run sort --config ./config.yaml --dry
```

Dry mode shows actions without applying changes.

## Development

Format and run tests:

```bash
go fmt ./... && go vet ./... && go test ./...
```

---

## License

LLM Tasks is released under the [MIT License](MIT-LICENSE).
