package main

import "testing"

func TestSanitizeRemoteFilename(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "plain", input: "photo.jpg"},
		{name: "empty", input: "", wantErr: true},
		{name: "dot", input: ".", wantErr: true},
		{name: "dotdot", input: "..", wantErr: true},
		{name: "unix path", input: "../photo.jpg", wantErr: true},
		{name: "windows path", input: `..\photo.jpg`, wantErr: true},
		{name: "nested path", input: "dir/photo.jpg", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeRemoteFilename(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.input {
				t.Fatalf("expected %q, got %q", tt.input, got)
			}
		})
	}
}
