package generate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const vetoModulePath = "github.com/aiveto/veto"

var errNoModuleVersion = errors.New("veto build has no release version")

// moduleVersion is the version a generated go.mod can require outside this checkout.
// A release is used as-is. A local replace does not hide that release.
// An untagged commit becomes a pseudo-version. v0.0.0 is not a stand-in.
func moduleVersion() (string, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "", errNoModuleVersion
	}
	if v := releasedVersion(info); v != "" {
		return v, nil
	}
	if v, err := moduleGitVersion(); err == nil && v != "" {
		return v, nil
	}
	return versionFrom(info)
}

// versionFrom is the veto module version recorded in build info.
// A host program's own version is not a veto release.
func versionFrom(info *debug.BuildInfo) (string, error) {
	if info == nil {
		return "", errNoModuleVersion
	}
	if v := releasedVersion(info); v != "" {
		return v, nil
	}
	if info.Main.Path != vetoModulePath {
		return "", errNoModuleVersion
	}
	rev, when, ok := vcsStamp(info)
	if !ok {
		return "", errNoModuleVersion
	}
	return pseudoVersion("", when, rev), nil
}

func releasedVersion(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	if info.Main.Path == vetoModulePath {
		return releaseVersion(info.Main.Version)
	}
	for _, dep := range info.Deps {
		if dep == nil || dep.Path != vetoModulePath {
			continue
		}
		if v := releaseVersion(dep.Version); v != "" {
			return v
		}
		if dep.Replace != nil {
			return releaseVersion(dep.Replace.Version)
		}
		return ""
	}
	return ""
}

func releaseVersion(v string) string {
	if len(v) < 2 || v[0] != 'v' || !strings.Contains(v, ".") || strings.Contains(v, "devel") {
		return ""
	}
	return v
}

func vcsStamp(info *debug.BuildInfo) (string, time.Time, bool) {
	var rev, raw string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			rev = setting.Value
		case "vcs.time":
			raw = setting.Value
		}
	}
	short, ok := shortRev(rev)
	if !ok || raw == "" {
		return "", time.Time{}, false
	}
	when, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", time.Time{}, false
	}
	return short, when.UTC().Truncate(time.Second), true
}

func shortRev(rev string) (string, bool) {
	if len(rev) < 12 {
		return "", false
	}
	rev = strings.ToLower(rev[:12])
	for _, c := range rev {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	return rev, true
}

func pseudoVersion(older string, when time.Time, rev string) string {
	segment := when.UTC().Format("20060102150405") + "-" + rev
	if older == "" {
		return "v0.0.0-" + segment
	}
	i := strings.LastIndex(older, ".")
	return older[:i+1] + incDecimal(older[i+1:]) + "-0." + segment
}

func incDecimal(decimal string) string {
	digits := []byte(decimal)
	i := len(digits) - 1
	for ; i >= 0 && digits[i] == '9'; i-- {
		digits[i] = '0'
	}
	if i >= 0 {
		digits[i]++
		return string(digits)
	}
	return "1" + string(digits)
}

func moduleGitVersion() (string, error) {
	dir, err := vetoModuleDir()
	if err != nil {
		return "", err
	}
	out, err := git(dir, "log", "-1", "--format=format:%H%n%ct")
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return "", errNoModuleVersion
	}
	rev, ok := shortRev(lines[0])
	if !ok {
		return "", errNoModuleVersion
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return "", errNoModuleVersion
	}
	when := time.Unix(sec, 0).UTC()
	if tag, ok := highestReleaseTag(dir, "tag", "--points-at", "HEAD"); ok {
		return tag, nil
	}
	if base, ok := highestReleaseTag(dir, "tag", "--merged", "HEAD"); ok {
		return pseudoVersion(base, when, rev), nil
	}
	return pseudoVersion("", when, rev), nil
}

func highestReleaseTag(dir string, args ...string) (string, bool) {
	out, err := git(dir, args...)
	if err != nil {
		return "", false
	}
	var best string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		tag := strings.TrimSpace(line)
		if !releaseTag(tag) {
			continue
		}
		if best == "" || versionLess(best, tag) {
			best = tag
		}
	}
	return best, best != ""
}

func releaseTag(tag string) bool {
	if len(tag) < 6 || tag[0] != 'v' {
		return false
	}
	parts := strings.Split(tag[1:], ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func versionLess(a, b string) bool {
	left := strings.Split(a[1:], ".")
	right := strings.Split(b[1:], ".")
	if len(left) != len(right) {
		return a < b
	}
	for i := range left {
		ln, lerr := strconv.Atoi(left[i])
		rn, rerr := strconv.Atoi(right[i])
		if lerr != nil || rerr != nil {
			return a < b
		}
		if ln != rn {
			return ln < rn
		}
	}
	return false
}

func vetoModuleDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errNoModuleVersion
	}
	dir := filepath.Dir(file)
	for {
		mod := filepath.Join(dir, "go.mod")
		raw, err := os.ReadFile(mod)
		if err == nil {
			if !strings.HasPrefix(string(raw), "module "+vetoModulePath+"\n") {
				return "", errNoModuleVersion
			}
			return dir, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNoModuleVersion
		}
		dir = parent
	}
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
