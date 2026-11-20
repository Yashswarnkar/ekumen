package mapview

import (
	"regexp"
	"strings"
)

// Links from the build map to source files on the git host (T-log-fixes
// item 8). Kept vendor-neutral: one URL template, with GitHub as the only
// built-in pattern; any other host is given with `ekumen map --link-base`.

// hostPatterns turn a remote URL into "<host>/<owner>/<repo>". Only
// GitHub is built in.
var hostPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^git@(github\.com):([^/]+)/([^/]+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^ssh://git@(github\.com)(?::\d+)?/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^https?://(?:[^@/]+@)?(github\.com)/([^/]+)/([^/]+?)(?:\.git)?/?$`),
}

// LinkBase returns the URL prefix for files on branch, e.g.
// "https://github.com/o/r/blob/main/", built from a git remote URL
// (git@github.com:o/r.git or https://github.com/o/r(.git)). It returns ""
// for a remote it does not recognize. An empty branch means "main".
func LinkBase(remote, branch string) string {
	remote = strings.TrimSpace(remote)
	if branch == "" {
		branch = "main"
	}
	for _, re := range hostPatterns {
		if m := re.FindStringSubmatch(remote); m != nil {
			return "https://" + m[1] + "/" + m[2] + "/" + m[3] + "/blob/" + branch + "/"
		}
	}
	return ""
}

// FileURL joins a link base and a repository-relative path. It returns ""
// when base or path is empty, so the page shows plain text.
func FileURL(base, path string) string {
	if base == "" || path == "" {
		return ""
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + strings.TrimPrefix(path, "/")
}

// RepoName is the last path element of a remote URL without ".git"
// (".../o/ekumen.git" gives "ekumen"), or "" for an empty remote.
func RepoName(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), "/")
	if remote == "" {
		return ""
	}
	if i := strings.LastIndexAny(remote, "/:"); i >= 0 {
		remote = remote[i+1:]
	}
	return strings.TrimSuffix(remote, ".git")
}
