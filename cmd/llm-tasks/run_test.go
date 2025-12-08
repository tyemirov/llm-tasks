package llmtasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmtasks "github.com/tyemirov/llm-tasks/cmd/llm-tasks"
)

const (
	changelogVersionFlagIdentifier = "version"
	changelogDateFlagIdentifier    = "date"
	openAIAPIKeyEnvName            = "OPENAI_API_KEY"
	changelogVersionEnvName        = "CHANGELOG_VERSION"
	changelogDateEnvName           = "CHANGELOG_DATE"
	changelogGitLogSample          = "commit 123 Added feature\n"
	changelogApplySummaryPrefix    = "prepended changelog to"
	changelogConfigTemplate        = `common:
  api:
    endpoint: %s
    api_key_env: OPENAI_API_KEY
  defaults:
    attempts: 1
    timeout_seconds: 1

models:
  - name: stub
    provider: openai
    model_id: stub-model
    default: true
    supports_temperature: false
    default_temperature: 0.1
    max_completion_tokens: 1200

recipes:
  - name: changelog
    enabled: true
    model: stub
    type: task/changelog
    inputs:
      version:
        required: true
        env: CHANGELOG_VERSION
        default: ""
      date:
        required: true
        env: CHANGELOG_DATE
        default: ""
      git_log:
        required: true
        source: stdin
    recipe:
      system: "System prompt"
      format:
        heading: "## [${version}] - ${date}"
        sections:
          - title: "Highlights"
            min: 1
          - title: "Features ✨"
          - title: "Improvements ⚙️"
        footer: ""
      rules: [ ]
    apply:
      output_path: %s
      mode: prepend
      ensure_blank_line: false
`
	openAIAPIKeyValue = "test-key"
)

type chatCompletionRequestPayload struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type chatCompletionResponsePayload struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func TestRunCommandChangelogMetadataInjection(testingT *testing.T) {
	testCases := []struct {
		name               string
		versionFlag        string
		dateFlag           string
		preexistingVersion string
		preexistingDate    string
		expectedVersion    string
		expectedDate       string
	}{
		{
			name:               "FlagsProvideVersionAndDate",
			versionFlag:        "v1.2.3",
			dateFlag:           "2024-03-15",
			preexistingVersion: "",
			preexistingDate:    "",
			expectedVersion:    "v1.2.3",
			expectedDate:       "2024-03-15",
		},
		{
			name:               "FlagOverridesVersionPreservesDate",
			versionFlag:        "v9.9.9",
			dateFlag:           "",
			preexistingVersion: "1.0.0",
			preexistingDate:    "2025-12-31",
			expectedVersion:    "v9.9.9",
			expectedDate:       "2025-12-31",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		testingT.Run(testCase.name, func(subTestT *testing.T) {
			serverInteractions := struct {
				requestPrompt string
				wasCalled     bool
			}{}

			mockServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				serverInteractions.wasCalled = true

				var payload chatCompletionRequestPayload
				decoderErr := json.NewDecoder(request.Body).Decode(&payload)
				if decoderErr != nil {
					subTestT.Fatalf("decode chat request: %v", decoderErr)
				}
				if len(payload.Messages) < 2 {
					subTestT.Fatalf("expected at least system and user messages, got %d", len(payload.Messages))
				}
				serverInteractions.requestPrompt = payload.Messages[1].Content

				responseText := fmt.Sprintf("## [%s] - %s\n\n### Highlights\n\n- Item\n\n### Features ✨\n\n- Feature\n\n### Improvements ⚙️\n\n- Improvement\n", testCase.expectedVersion, testCase.expectedDate)

				responsePayload := chatCompletionResponsePayload{
					Choices: []struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					}{
						{
							Message: struct {
								Content string `json:"content"`
							}{Content: responseText},
						},
					},
				}

				responseWriter.Header().Set("Content-Type", "application/json")
				encoderErr := json.NewEncoder(responseWriter).Encode(responsePayload)
				if encoderErr != nil {
					subTestT.Fatalf("encode chat response: %v", encoderErr)
				}
			}))
			defer mockServer.Close()

			temporaryDirectory := subTestT.TempDir()
			configPath := filepath.Join(temporaryDirectory, "config.yaml")
			changelogPath := filepath.Join(temporaryDirectory, "CHANGELOG.md")

			configContent := fmt.Sprintf(changelogConfigTemplate, mockServer.URL, changelogPath)

			writeErr := os.WriteFile(configPath, []byte(configContent), 0o600)
			if writeErr != nil {
				subTestT.Fatalf("write config: %v", writeErr)
			}

			subTestT.Setenv(openAIAPIKeyEnvName, openAIAPIKeyValue)
			subTestT.Setenv(changelogVersionEnvName, testCase.preexistingVersion)
			subTestT.Setenv(changelogDateEnvName, testCase.preexistingDate)

			stdinReader, stdinWriter, pipeErr := os.Pipe()
			if pipeErr != nil {
				subTestT.Fatalf("create stdin pipe: %v", pipeErr)
			}
			if _, writeErr := stdinWriter.WriteString(changelogGitLogSample); writeErr != nil {
				subTestT.Fatalf("seed stdin: %v", writeErr)
			}
			if closeErr := stdinWriter.Close(); closeErr != nil {
				subTestT.Fatalf("close stdin writer: %v", closeErr)
			}
			originalStdin := os.Stdin
			os.Stdin = stdinReader
			subTestT.Cleanup(func() {
				_ = stdinReader.Close()
				os.Stdin = originalStdin
			})

			command := llmtasks.NewRootCommand()
			command.SetArgs(buildRunArguments(configPath, testCase.versionFlag, testCase.dateFlag))
			command.SetIn(strings.NewReader(changelogGitLogSample))
			var outputBuffer bytes.Buffer
			command.SetOut(&outputBuffer)
			command.SetErr(&outputBuffer)

			executeErr := command.Execute()
			if executeErr != nil {
				subTestT.Fatalf("execute run command: %v\noutput:%s", executeErr, outputBuffer.String())
			}

			if !serverInteractions.wasCalled {
				subTestT.Fatalf("expected mock server to be invoked")
			}

			expectedHeading := fmt.Sprintf("## [%s] - %s", testCase.expectedVersion, testCase.expectedDate)
			if !strings.Contains(serverInteractions.requestPrompt, expectedHeading) {
				subTestT.Fatalf("expected prompt to contain heading %q, got %q", expectedHeading, serverInteractions.requestPrompt)
			}

			changelogData, readErr := os.ReadFile(changelogPath)
			if readErr != nil {
				subTestT.Fatalf("read changelog output: %v", readErr)
			}
			if !strings.HasPrefix(string(changelogData), expectedHeading) {
				subTestT.Fatalf("expected changelog file to start with %q, got %q", expectedHeading, string(changelogData))
			}

			if os.Getenv(changelogVersionEnvName) != testCase.expectedVersion {
				subTestT.Fatalf("expected CHANGELOG_VERSION to equal %q, got %q", testCase.expectedVersion, os.Getenv(changelogVersionEnvName))
			}
			if os.Getenv(changelogDateEnvName) != testCase.expectedDate {
				subTestT.Fatalf("expected CHANGELOG_DATE to equal %q, got %q", testCase.expectedDate, os.Getenv(changelogDateEnvName))
			}

			if !strings.Contains(outputBuffer.String(), changelogApplySummaryPrefix) {
				subTestT.Fatalf("expected command output to confirm changelog application, got: %s", outputBuffer.String())
			}
		})
	}
}

func buildRunArguments(configPath, versionFlag, dateFlag string) []string {
	arguments := []string{"run", "changelog", "--config", configPath}
	if strings.TrimSpace(versionFlag) != "" {
		arguments = append(arguments, "--"+changelogVersionFlagIdentifier, versionFlag)
	}
	if strings.TrimSpace(dateFlag) != "" {
		arguments = append(arguments, "--"+changelogDateFlagIdentifier, dateFlag)
	}
	return arguments
}
