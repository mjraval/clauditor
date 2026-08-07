package doctor

import (
	"fmt"
	"regexp"
	"strconv"
)

// semverRe pulls a tolerant X.Y[.Z] out of arbitrary version-command output.
// tmux prints "tmux 3.4" (two components); claude and git print three
// ("2.1.223 (Claude Code)", "git version 2.43.0"). A missing patch defaults
// to 0.
var semverRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

type semver struct{ major, minor, patch int }

func (v semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// less reports whether v < other.
func (v semver) less(other semver) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}

// parseSemver extracts the first X.Y[.Z] substring from s. ok is false when
// no version-shaped substring is found.
func parseSemver(s string) (v semver, ok bool) {
	m := semverRe.FindStringSubmatch(s)
	if m == nil {
		return semver{}, false
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return semver{}, false
	}
	patch := 0
	if m[3] != "" {
		if p, err := strconv.Atoi(m[3]); err == nil {
			patch = p
		}
	}
	return semver{major, minor, patch}, true
}
