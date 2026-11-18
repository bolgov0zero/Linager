package release

import "testing"

func TestPreReleaseNumbersAreCompared(t *testing.T) {
	// the order of the real beta releases: 9 is below 10 and 11 (as text it was above)
	ordered := []string{"v0.3.0-beta.1", "v0.3.0-beta.2", "v0.3.0-beta.9", "v0.3.0-beta.10", "v0.3.0-beta.11", "v0.3.0-beta.12", "v0.3.0-rc.1", "v0.3.0", "v0.3.1-beta.1"}
	for i := 0; i < len(ordered); i++ {
		for j := 0; j < len(ordered); j++ {
			got := Compare(ParseVersion(ordered[i]), ParseVersion(ordered[j]))
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
	for _, c := range []struct{ newer, older string }{
		{"v0.3.0-beta.11", "v0.3.0-beta.9"}, {"v0.3.0-beta.10", "v0.3.0-beta.2"}, {"v1.0.0-rc.10", "v1.0.0-rc.9"},
		{"v1.0.0-beta.1", "v1.0.0-beta"}, {"v1.0.0-beta", "v1.0.0-alpha"}, {"v1.0.0-alpha", "v1.0.0-1"}, {"v1.0.0", "v1.0.0-rc.1"},
	} {
		if !Newer(c.newer, c.older) || Newer(c.older, c.newer) {
			t.Errorf("%s must be newer than %s", c.newer, c.older)
		}
	}
}
