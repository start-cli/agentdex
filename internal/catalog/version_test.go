package catalog

import "testing"

func TestLatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
		wantErr  bool
	}{
		{"single", []string{"v1.0.0"}, "v1.0.0", false},
		{"highest stable", []string{"v1.0.0", "v1.1.0", "v1.0.3"}, "v1.1.0", false},
		{"major ordering", []string{"v1.0.0", "v2.0.0"}, "v2.0.0", false},
		{"stable beats higher prerelease", []string{"v1.0.0", "v1.2.0-rc1"}, "v1.0.0", false},
		{"prerelease only falls back to highest", []string{"v1.2.0-rc1", "v1.2.0-rc2"}, "v1.2.0-rc2", false},
		{"invalid skipped", []string{"garbage", "v1.0.0"}, "v1.0.0", false},
		{"empty", nil, "", true},
		{"all invalid", []string{"garbage", "notaversion"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := latestVersion(tt.versions)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("latestVersion(%v) = %q, want error", tt.versions, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("latestVersion(%v): %v", tt.versions, err)
			}
			if got != tt.want {
				t.Errorf("latestVersion(%v) = %q, want %q", tt.versions, got, tt.want)
			}
		})
	}
}
