package transfer

import "testing"

func TestShouldCompress(t *testing.T) {
	tests := []struct {
		name string
		file string
		mode string
		want bool
	}{
		{name: "auto text", file: "notes.txt", mode: CompressAuto, want: true},
		{name: "auto video", file: "clip.mp4", mode: CompressAuto, want: false},
		{name: "auto archive", file: "payload.tar.zst", mode: CompressAuto, want: false},
		{name: "always overrides", file: "clip.mp4", mode: CompressAlways, want: true},
		{name: "never overrides", file: "notes.txt", mode: CompressNever, want: false},
		{name: "invalid mode defaults auto", file: "notes.txt", mode: "bad", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCompress(tt.file, tt.mode); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestNormalizeCompressionMode(t *testing.T) {
	if mode, ok := NormalizeCompressionMode(""); !ok || mode != CompressAuto {
		t.Fatalf("empty mode should normalize to auto")
	}
	if _, ok := NormalizeCompressionMode("bad"); ok {
		t.Fatalf("invalid mode should fail")
	}
}
