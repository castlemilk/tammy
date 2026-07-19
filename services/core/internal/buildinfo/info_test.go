package buildinfo

import "testing"

func TestCurrent(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    Info
	}{
		{
			name: "defaults an empty version to dev",
			want: Info{Version: "dev"},
		},
		{
			name:    "preserves a supplied version",
			version: "0.1.0-test+linker",
			want:    Info{Version: "0.1.0-test+linker"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalVersion := version
			t.Cleanup(func() {
				version = originalVersion
			})

			version = tt.version

			if got := Current(); got != tt.want {
				t.Fatalf("Current() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
