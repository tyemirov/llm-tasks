package sort

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tyemirov/llm-tasks/internal/config"
)

func TestResolveSortGrantBaseDirectories(t *testing.T) {
	testCases := []struct {
		name                string
		source              config.Sort
		environmentValues   map[string]string
		expected            config.Sort
		expectedErrorSubstr string
	}{
		{
			name: "expands placeholders when environment variables are set",
			source: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "${SORT_DOWNLOADS_DIR}/incoming"
				sortConfiguration.Grant.BaseDirectories.Staging = "${SORT_STAGING_DIR}"
				return sortConfiguration
			}(),
			environmentValues: map[string]string{
				"SORT_DOWNLOADS_DIR": "/var/downloads",
				"SORT_STAGING_DIR":   "/var/staging",
			},
			expected: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "/var/downloads/incoming"
				sortConfiguration.Grant.BaseDirectories.Staging = "/var/staging"
				return sortConfiguration
			}(),
		},
		{
			name: "returns error when required environment variable is missing",
			source: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "${SORT_DOWNLOADS_DIR}"
				sortConfiguration.Grant.BaseDirectories.Staging = "${SORT_STAGING_DIR}"
				return sortConfiguration
			}(),
			environmentValues: map[string]string{
				"SORT_STAGING_DIR": "/tmp/staging",
			},
			expectedErrorSubstr: "SORT_DOWNLOADS_DIR",
		},
		{
			name: "leaves literal paths unchanged when no placeholders provided",
			source: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "/opt/downloads"
				sortConfiguration.Grant.BaseDirectories.Staging = "/opt/staging"
				return sortConfiguration
			}(),
			environmentValues: map[string]string{},
			expected: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "/opt/downloads"
				sortConfiguration.Grant.BaseDirectories.Staging = "/opt/staging"
				return sortConfiguration
			}(),
		},
		{
			name: "returns error when downloads directory literal is blank",
			source: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "   "
				sortConfiguration.Grant.BaseDirectories.Staging = "/opt/staging"
				return sortConfiguration
			}(),
			environmentValues:   map[string]string{},
			expectedErrorSubstr: sortGrantDownloadsDirectoryKey,
		},
		{
			name: "returns error when downloads directory expansion is blank",
			source: func() config.Sort {
				var sortConfiguration config.Sort
				sortConfiguration.Grant.BaseDirectories.Downloads = "${EMPTY_DOWNLOADS_DIR}"
				sortConfiguration.Grant.BaseDirectories.Staging = "/opt/staging"
				return sortConfiguration
			}(),
			environmentValues: map[string]string{
				"EMPTY_DOWNLOADS_DIR": "  ",
			},
			expectedErrorSubstr: sortGrantDownloadsDirectoryKey,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			lookup := func(variableName string) (string, bool) {
				if testCase.environmentValues == nil {
					return "", false
				}
				value, exists := testCase.environmentValues[variableName]
				return value, exists
			}

			resolved, err := resolveSortGrantBaseDirectories(testCase.source, lookup)
			if testCase.expectedErrorSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", testCase.expectedErrorSubstr)
				}
				if !strings.Contains(err.Error(), testCase.expectedErrorSubstr) {
					t.Fatalf("error %q does not contain expected substring %q", err.Error(), testCase.expectedErrorSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(resolved, testCase.expected) {
				t.Fatalf("resolved configuration mismatch\nexpected: %#v\nactual: %#v", testCase.expected, resolved)
			}
		})
	}
}
