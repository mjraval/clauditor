package doctor

import "testing"

func TestParseSemver(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  semver
		ok    bool
	}{
		{"claude real stub format", "2.1.223 (Claude Code)", semver{2, 1, 223}, true},
		{"bare three-component", "2.1.139", semver{2, 1, 139}, true},
		{"tmux two-component", "tmux 3.4", semver{3, 4, 0}, true},
		{"git prefixed", "git version 2.43.0", semver{2, 43, 0}, true},
		{"leading text and trailing text", "clauditor-stub v10.2.5-beta", semver{10, 2, 5}, true},
		{"unparseable, no digits", "not a version string", semver{}, false},
		{"unparseable, single number", "42", semver{}, false},
		{"empty string", "", semver{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSemver(tc.input)
			if ok != tc.ok {
				t.Fatalf("parseSemver(%q) ok = %v, want %v", tc.input, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("parseSemver(%q) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSemverLess(t *testing.T) {
	tests := []struct {
		a, b semver
		want bool
	}{
		{semver{2, 1, 138}, semver{2, 1, 139}, true},
		{semver{2, 1, 139}, semver{2, 1, 139}, false},
		{semver{2, 1, 140}, semver{2, 1, 139}, false},
		{semver{2, 0, 999}, semver{2, 1, 0}, true},
		{semver{1, 9, 999}, semver{2, 0, 0}, true},
		{semver{3, 4, 0}, semver{3, 0, 0}, false},
	}
	for _, tc := range tests {
		if got := tc.a.less(tc.b); got != tc.want {
			t.Errorf("%v.less(%v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
