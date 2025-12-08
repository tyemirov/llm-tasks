package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tyemirov/llm-tasks/internal/config"
)

const (
	explicitConfigurationFileName       = "explicit.yaml"
	workingDirectoryConfigurationName   = "config.yaml"
	homeDirectoryName                   = ".llm-tasks"
	homeConfigurationFileName           = "config.yaml"
	sampleAPIEndpoint                   = "https://example.test/api"
	sampleAPIKeyEnvironmentVariableName = "EXAMPLE_API_KEY"
	explicitLoggingLevel                = "explicit-level"
	workingLoggingLevel                 = "working-level"
	homeLoggingLevel                    = "home-level"
	embeddedLoggingLevel                = "info"
	embeddedSortRecipeName              = "sort"
	embeddedChangelogRecipeName         = "changelog"
	missingExplicitFileName             = "missing.yaml"
	configurationTemplate               = "common:\n  api:\n    endpoint: %s\n    api_key_env: %s\n  logging:\n    level: %s\n    format: console\n  defaults:\n    attempts: 1\n    timeout_seconds: 2\nmodels:\n  - name: default\n    provider: provider\n    model_id: model\n    default: true\n    supports_temperature: true\n    default_temperature: 0.1\n    max_completion_tokens: 10\nrecipes:\n  - name: sample\n    enabled: true\n    type: task/sort\n"
	directoryPermissions                = 0o755
	filePermissions                     = 0o644
	embeddedRecipeMissingErrorFormat    = "expected recipe %s to be available"
	sanitizedSortDownloadsPlaceholder   = "${SORT_DOWNLOADS_DIR}"
	sanitizedSortStagingPlaceholder     = "${SORT_STAGING_DIR}"
	sortGrantDownloadsKey               = "downloads"
	sortGrantStagingKey                 = "staging"
	missingSortGrantBaseDirectoryFormat = "missing sort grant base directory %s"
	unexpectedSortGrantPathFormat       = "expected sort grant base directory %s to be %s, got %s"
	sortRecipeMappingFailureFormat      = "map sort recipe: %v"
)

type loaderTestCase struct {
	name                             string
	setup                            func(t *testing.T, workingDirectory string, homeDirectory string) (string, string)
	expectedLoggingLevel             string
	expectedRecipeNames              []string
	expectedSortGrantBaseDirectories map[string]string
}

func TestRootConfigurationLoader_Load(t *testing.T) {
	testCases := []loaderTestCase{
		{
			name: "explicit path used when available",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				configurationPath := filepath.Join(workingDirectory, explicitConfigurationFileName)
				writeConfiguration(t, configurationPath, explicitLoggingLevel)
				return configurationPath, configurationPath
			},
			expectedLoggingLevel: explicitLoggingLevel,
		},
		{
			name: "explicit path missing falls back to working directory",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				workingConfigurationPath := filepath.Join(workingDirectory, workingDirectoryConfigurationName)
				writeConfiguration(t, workingConfigurationPath, workingLoggingLevel)
				explicitPath := filepath.Join(workingDirectory, missingExplicitFileName)
				return explicitPath, workingConfigurationPath
			},
			expectedLoggingLevel: workingLoggingLevel,
		},
		{
			name: "working directory used when explicit path not provided",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				workingConfigurationPath := filepath.Join(workingDirectory, workingDirectoryConfigurationName)
				writeConfiguration(t, workingConfigurationPath, workingLoggingLevel)
				return "", workingConfigurationPath
			},
			expectedLoggingLevel: workingLoggingLevel,
		},
		{
			name: "home directory used when other locations missing",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				configurationDirectory := filepath.Join(homeDirectory, homeDirectoryName)
				configurationPath := filepath.Join(configurationDirectory, homeConfigurationFileName)
				writeConfiguration(t, configurationPath, homeLoggingLevel)
				return "", configurationPath
			},
			expectedLoggingLevel: homeLoggingLevel,
		},
		{
			name: "embedded configuration used when no files available",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				return "", config.EmbeddedRootConfigurationReference
			},
			expectedLoggingLevel: embeddedLoggingLevel,
			expectedRecipeNames:  []string{embeddedSortRecipeName, embeddedChangelogRecipeName},
			expectedSortGrantBaseDirectories: map[string]string{
				sortGrantDownloadsKey: sanitizedSortDownloadsPlaceholder,
				sortGrantStagingKey:   sanitizedSortStagingPlaceholder,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			workingDirectory := t.TempDir()
			homeDirectory := t.TempDir()

			loader := config.NewRootConfigurationLoader(workingDirectory, homeDirectory)
			explicitPath, expectedReference := testCase.setup(t, workingDirectory, homeDirectory)

			source, loadErr := loader.Load(explicitPath)
			if loadErr != nil {
				t.Fatalf("load configuration source: %v", loadErr)
			}
			if expectedReference != "" && source.Reference != expectedReference {
				t.Fatalf("expected reference %s, got %s", expectedReference, source.Reference)
			}

			rootConfiguration, parseErr := config.LoadRoot(source)
			if parseErr != nil {
				t.Fatalf("parse root configuration: %v", parseErr)
			}
			if rootConfiguration.Common.Logging.Level != testCase.expectedLoggingLevel {
				t.Fatalf("expected logging level %s, got %s", testCase.expectedLoggingLevel, rootConfiguration.Common.Logging.Level)
			}
			for _, expectedRecipeName := range testCase.expectedRecipeNames {
				if _, recipeFound := rootConfiguration.FindRecipe(expectedRecipeName); !recipeFound {
					t.Fatalf(embeddedRecipeMissingErrorFormat, expectedRecipeName)
				}
			}
			if len(testCase.expectedSortGrantBaseDirectories) > 0 {
				sortRecipe, sortRecipeFound := rootConfiguration.FindRecipe(embeddedSortRecipeName)
				if !sortRecipeFound {
					t.Fatalf(embeddedRecipeMissingErrorFormat, embeddedSortRecipeName)
				}
				sortConfiguration, sortConfigurationMapError := config.MapSort(sortRecipe)
				if sortConfigurationMapError != nil {
					t.Fatalf(sortRecipeMappingFailureFormat, sortConfigurationMapError)
				}
				actualGrantDirectories := map[string]string{
					sortGrantDownloadsKey: sortConfiguration.Grant.BaseDirectories.Downloads,
					sortGrantStagingKey:   sortConfiguration.Grant.BaseDirectories.Staging,
				}
				for directoryKey, expectedPath := range testCase.expectedSortGrantBaseDirectories {
					actualPath, directoryFound := actualGrantDirectories[directoryKey]
					if !directoryFound {
						t.Fatalf(missingSortGrantBaseDirectoryFormat, directoryKey)
					}
					if actualPath != expectedPath {
						t.Fatalf(unexpectedSortGrantPathFormat, directoryKey, expectedPath, actualPath)
					}
				}
			}
		})
	}
}

func TestRootConfigurationLoader_Load_UnreadableCandidates(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(t *testing.T, workingDirectory string, homeDirectory string) (string, string)
	}{
		{
			name: "explicit configuration path unreadable",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				configurationDirectory := filepath.Join(workingDirectory, explicitConfigurationFileName)
				if err := os.MkdirAll(configurationDirectory, directoryPermissions); err != nil {
					t.Fatalf("create explicit configuration directory: %v", err)
				}
				return configurationDirectory, configurationDirectory
			},
		},
		{
			name: "working directory configuration unreadable",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				configurationDirectory := filepath.Join(workingDirectory, workingDirectoryConfigurationName)
				if err := os.MkdirAll(configurationDirectory, directoryPermissions); err != nil {
					t.Fatalf("create working configuration directory: %v", err)
				}
				return "", filepath.Join(workingDirectory, workingDirectoryConfigurationName)
			},
		},
		{
			name: "home directory configuration unreadable",
			setup: func(t *testing.T, workingDirectory string, homeDirectory string) (string, string) {
				t.Helper()
				configurationDirectory := filepath.Join(homeDirectory, homeDirectoryName, homeConfigurationFileName)
				if err := os.MkdirAll(configurationDirectory, directoryPermissions); err != nil {
					t.Fatalf("create home configuration directory: %v", err)
				}
				return "", filepath.Join(homeDirectory, homeDirectoryName, homeConfigurationFileName)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			workingDirectory := t.TempDir()
			homeDirectory := t.TempDir()

			loader := config.NewRootConfigurationLoader(workingDirectory, homeDirectory)
			explicitPath, expectedReference := testCase.setup(t, workingDirectory, homeDirectory)

			_, loadErr := loader.Load(explicitPath)
			if loadErr == nil {
				t.Fatalf("expected load error for unreadable configuration candidate")
			}
			if !strings.Contains(loadErr.Error(), expectedReference) {
				t.Fatalf("expected load error to reference %s, got %v", expectedReference, loadErr)
			}
		})
	}
}

func writeConfiguration(t *testing.T, path string, loggingLevel string) {
	t.Helper()
	configurationDirectory := filepath.Dir(path)
	if err := os.MkdirAll(configurationDirectory, directoryPermissions); err != nil {
		t.Fatalf("create configuration directory: %v", err)
	}
	content := fmt.Sprintf(configurationTemplate, sampleAPIEndpoint, sampleAPIKeyEnvironmentVariableName, loggingLevel)
	if err := os.WriteFile(path, []byte(content), filePermissions); err != nil {
		t.Fatalf("write configuration file: %v", err)
	}
}
