package main

import (
	"reflect"
	"testing"
)

func TestResolveRouting(t *testing.T) {
	fakeGhPath := "/usr/local/bin/gh-real"

	tests := []struct {
		name        string
		inputArgs   []string
		wantCmd     string
		wantCmdArgs []string
	}{
		{
			name:        "No arguments defaults to gh",
			inputArgs:   []string{},
			wantCmd:     fakeGhPath,
			wantCmdArgs: []string{},
		},
		{
			name:        "Explicit separator passes remaining to gh",
			inputArgs:   []string{"--", "pr", "list"},
			wantCmd:     fakeGhPath,
			wantCmdArgs: []string{"pr", "list"},
		},
		{
			name:        "Core gh subcommand stays with gh (prevents /usr/bin/pr collision)",
			inputArgs:   []string{"pr", "--repo", "owner/repo"},
			wantCmd:     fakeGhPath,
			wantCmdArgs: []string{"pr", "--repo", "owner/repo"},
		},
		{
			name:        "Custom configuration alias stays with gh",
			inputArgs:   []string{"co", "123"},
			wantCmd:     fakeGhPath,
			wantCmdArgs: []string{"co", "123"},
		},
		{
			name:        "Global flags stay with gh",
			inputArgs:   []string{"--help"},
			wantCmd:     fakeGhPath,
			wantCmdArgs: []string{"--help"},
		},
		{
			name:        "Standard external utility shifts to path binary",
			inputArgs:   []string{"git", "status"},
			wantCmd:     "/usr/bin/git", // Resolved dynamically via exec.LookPath
			wantCmdArgs: []string{"status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotCmdArgs := resolveRouting(tt.inputArgs, fakeGhPath)

			// Clean check: safe guard against empty slices before index evaluation
			if len(tt.inputArgs) > 0 && tt.inputArgs[0] == "git" && gotCmd != "" {
				if !reflect.DeepEqual(gotCmdArgs, tt.wantCmdArgs) {
					t.Errorf("resolveRouting() gotCmdArgs = %v, want %v", gotCmdArgs, tt.wantCmdArgs)
				}
				return
			}

			if gotCmd != tt.wantCmd {
				t.Errorf("resolveRouting() gotCmd = %v, want %v", gotCmd, tt.wantCmd)
			}
			if !reflect.DeepEqual(gotCmdArgs, tt.wantCmdArgs) {
				t.Errorf("resolveRouting() gotCmdArgs = %v, want %v", gotCmdArgs, tt.wantCmdArgs)
			}
		})
	}
}
