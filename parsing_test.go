package main

import "testing"

func TestSplitPathAndMask(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantDir    string
		wantMask   string
		wantParsed bool
	}{
		{
			name:       "unix absolute path with mask",
			input:      "/tmp/foo/*.txt",
			wantDir:    "/tmp/foo",
			wantMask:   "*.txt",
			wantParsed: true,
		},
		{
			name:       "windows absolute path with mask",
			input:      `Y:\dev\projects\dir\*.7z`,
			wantDir:    `Y:\dev\projects\dir`,
			wantMask:   "*.7z",
			wantParsed: true,
		},
		{
			name:       "windows root path with mask",
			input:      `C:\*.txt`,
			wantDir:    `C:\`,
			wantMask:   "*.txt",
			wantParsed: true,
		},
		{
			name:       "archive path with internal slash separator",
			input:      `C:\archives\foo.zip/*.md`,
			wantDir:    `C:\archives\foo.zip`,
			wantMask:   "*.md",
			wantParsed: true,
		},
		{
			name:       "filename only",
			input:      "*.go",
			wantDir:    "",
			wantMask:   "",
			wantParsed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDir, gotMask, gotParsed := splitPathAndMask(tc.input)
			if gotParsed != tc.wantParsed || gotDir != tc.wantDir || gotMask != tc.wantMask {
				t.Fatalf("splitPathAndMask(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.input, gotDir, gotMask, gotParsed, tc.wantDir, tc.wantMask, tc.wantParsed)
			}
		})
	}
}
