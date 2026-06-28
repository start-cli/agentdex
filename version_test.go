package agentdex

import (
	"strings"
	"testing"
)

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		pattern string
		want    string
	}{
		{"no pattern returns trimmed output", "  1.2.3\n", "", "1.2.3"},
		{"capture group returned", "myagent v1.2.3", `v([0-9.]+)`, "1.2.3"},
		{"whole match when no capture group", "build 9.9.9 here", `[0-9]+\.[0-9]+\.[0-9]+`, "9.9.9"},
		{"no match yields empty", "no version here", `v([0-9.]+)`, ""},
		{"uncompilable pattern yields empty", "1.2.3", `v([0-9.+`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractVersion(tt.output, tt.pattern); got != tt.want {
				t.Errorf("extractVersion(%q, %q) = %q, want %q", tt.output, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestCappedBufferStopsAtCap(t *testing.T) {
	b := &cappedBuffer{cap: 8}

	// A single oversized write keeps only the first cap bytes.
	n, err := b.Write([]byte("abcdefghij")) // 10 bytes into an 8-byte cap
	if err != nil || n != 10 {
		t.Fatalf("Write = %d, %v, want 10, nil (full write always reported)", n, err)
	}
	if b.String() != "abcdefgh" {
		t.Errorf("buffer = %q, want first 8 bytes", b.String())
	}

	// Further writes past the cap are discarded but still reported as written, so
	// the producing process is never blocked or faulted.
	n, err = b.Write([]byte("klmno"))
	if err != nil || n != 5 {
		t.Fatalf("Write = %d, %v, want 5, nil", n, err)
	}
	if b.String() != "abcdefgh" {
		t.Errorf("buffer = %q, want unchanged after the cap was reached", b.String())
	}
}

func TestCappedBufferBoundsFloodedOutput(t *testing.T) {
	b := &cappedBuffer{cap: maxVersionOutput}
	flood := strings.Repeat("x", maxVersionOutput*4)
	if _, err := b.Write([]byte(flood)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := len(b.String()); got != maxVersionOutput {
		t.Errorf("retained %d bytes, want capped at %d", got, maxVersionOutput)
	}
}
