package git

import "testing"

func TestParseVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line      string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{"git version 2.39.5", 2, 39, true},
		{"git version 2.40.0", 2, 40, true},
		{"git version 2.40", 2, 40, true},
		{"git version 2.39.5 (Apple Git-140)", 2, 39, true},
		{"git version 3.0.0", 3, 0, true},
		{"git version 3.1", 3, 1, true},
		{"git version 1.8.3.1", 1, 8, true},
		{"git", 0, 0, false},
		{"git version", 0, 0, false},
		{"git version abc", 0, 0, false},
		{"git version 2", 0, 0, false},
		{"git version -1.5", 0, 0, false},
		{"", 0, 0, false},
		{"git version 2.39.5\n", 2, 39, true},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			major, minor, ok := ParseVersion(tc.line)
			if ok != tc.wantOK || major != tc.wantMajor || minor != tc.wantMinor {
				t.Fatalf("ParseVersion(%q) = %d.%d ok=%v, want %d.%d ok=%v", tc.line, major, minor, ok, tc.wantMajor, tc.wantMinor, tc.wantOK)
			}
		})
	}
}

// TestParseVersionBoundaryPinsTheGatedMinimum keeps the skip guard and the
// runtime warning on the same boundary the branchsync merge-tree checks gate:
// 2.40 is new enough, 2.39 is not.
func TestParseVersionBoundaryPinsTheGatedMinimum(t *testing.T) {
	t.Parallel()
	if major, minor, ok := ParseVersion("git version 2.39.5"); !ok || major != 2 || minor != 39 {
		t.Fatalf("2.39.5 should parse as 2.39, got %d.%d ok=%v", major, minor, ok)
	}
	if major, minor, ok := ParseVersion("git version 2.40.0"); !ok || major != MinVersionMajor || minor != MinVersionMinor {
		t.Fatalf("2.40.0 should parse as the declared minimum %d.%d, got %d.%d ok=%v", MinVersionMajor, MinVersionMinor, major, minor, ok)
	}
	if MinVersionMajor != 2 || MinVersionMinor != 40 {
		t.Fatalf("the declared minimum is %d.%d, not git 2.40", MinVersionMajor, MinVersionMinor)
	}
}
