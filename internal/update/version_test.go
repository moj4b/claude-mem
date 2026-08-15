package update

import "testing"

func TestIsRelease(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"v0.1.0", true},
		{"0.1.0", true},
		{"v1.20.300", true},
		{"v0.2.0-rc.1", true},
		{"v0.2.0+build.5", true},
		// Everything versionString() can produce for an unstamped build must
		// land here, or a local `go build` gets told to upgrade.
		{"(devel)", false},
		{"(devel 4d1a2b3c5e6f)", false},
		{"(devel 4d1a2b3c5e6f-dirty)", false},
		{"", false},
		{"v1.2", false},
		{"v1.2.3.4", false},
		{"v1.2.x", false},
		{"v01.2.3", false},
		{"v1.2.3-", false},
		{"latest", false},
	} {
		if got := IsRelease(tc.v); got != tc.want {
			t.Errorf("IsRelease(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.1", true},
		{"v0.9.9", "v1.0.0", true},
		{"v1.2.3", "v1.10.0", true}, // numeric, not lexical: 10 > 2
		{"v0.1.0", "v0.1.0", false},
		{"v0.2.0", "v0.1.0", false},
		{"0.1.0", "v0.2.0", true}, // the `v` is optional on either side
		{"v0.2.0-rc.1", "v0.2.0", true},
		{"v0.2.0", "v0.2.0-rc.1", false},
		{"v0.2.0-rc.1", "v0.2.0-rc.2", true},
		{"v0.2.0-rc.2", "v0.2.0-rc.10", true}, // numeric identifiers compare as numbers
		{"v0.2.0-alpha", "v0.2.0-beta", true},
		{"v0.2.0-rc", "v0.2.0-rc.1", true}, // a prefix loses to the longer list
		{"v0.1.0+a", "v0.1.0+b", false},    // build metadata is not ordered
		// Nothing comparable on either side means no claim at all.
		{"(devel)", "v9.9.9", false},
		{"v0.1.0", "", false},
		{"v0.1.0", "latest", false},
		{"", "", false},
	} {
		if got := Newer(tc.current, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}
