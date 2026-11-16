// Package update compares the installed version with GitHub releases.
// It does not download or run installers.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ReleasesAPI  = "https://api.github.com/repos/xujinheng/windows-ai-tool/releases?per_page=15"
	ReleasesPage = "https://github.com/xujinheng/windows-ai-tool/releases"
	checkTimeout = 8 * time.Second
)

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type Version struct {
	major, minor, patch int64
	pre                 string
}

type Release struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

type Notice struct {
	Latest string
	Newer  bool
	Found  bool
	Err    error
}

func Parse(s string) (Version, bool) {
	m := versionPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, false
	}
	major, ea := strconv.ParseInt(m[1], 10, 64)
	minor, eb := strconv.ParseInt(m[2], 10, 64)
	patch, ec := strconv.ParseInt(m[3], 10, 64)
	if ea != nil || eb != nil || ec != nil {
		return Version{}, false
	}
	return Version{major, minor, patch, m[4]}, true
}

func Compare(a, b Version) int {
	left := []int64{a.major, a.minor, a.patch}
	right := []int64{b.major, b.minor, b.patch}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	la, lb := strings.Split(a.pre, "."), strings.Split(b.pre, ".")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] == lb[i] {
			continue
		}
		an, ae := strconv.ParseUint(la[i], 10, 64)
		bn, be := strconv.ParseUint(lb[i], 10, 64)
		if ae == nil && be == nil {
			if an < bn {
				return -1
			}
			return 1
		}
		if ae == nil {
			return -1
		}
		if be == nil {
			return 1
		}
		if la[i] < lb[i] {
			return -1
		}
		return 1
	}
	if len(la) < len(lb) {
		return -1
	}
	return 1
}

func Select(current string, releases []Release) Notice {
	installed, ok := Parse(current)
	if !ok {
		return Notice{Err: fmt.Errorf("无法解析当前版本 %q", current)}
	}
	var latest Version
	notice := Notice{}
	for _, release := range releases {
		if release.Draft {
			continue
		}
		version, valid := Parse(release.TagName)
		if !valid {
			continue
		}
		if !notice.Found || Compare(version, latest) > 0 {
			latest = version
			notice.Found = true
			notice.Latest = release.TagName
		}
	}
	notice.Newer = notice.Found && Compare(latest, installed) > 0
	return notice
}

func (n Notice) Summary(current string) string {
	if n.Err != nil {
		return "检查更新失败：" + n.Err.Error()
	}
	if !n.Found {
		return "当前仓库尚未发布可下载的版本。\n" + ReleasesPage
	}
	if !n.Newer {
		return "当前版本：" + current + "\n最新发布：" + n.Latest + "\n已是最新版本。"
	}
	return Prompt(n.Latest)
}

func Prompt(latest string) string {
	return "发现新版本 " + latest + "。\n\n当前程序不会自动下载或运行未经签名的安装文件。\n是否打开 GitHub 官方发布页面？"
}

func Fetch(ctx context.Context, installed string, client *http.Client) Notice {
	if _, ok := Parse(installed); !ok {
		return Notice{Err: fmt.Errorf("未识别的版本 %q", installed)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesAPI, nil)
	if err != nil {
		return Notice{Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "VoiceRewrite-update-check/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return Notice{Err: errors.New("无法连接 GitHub，请检查网络或稍后再试")}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Notice{Err: fmt.Errorf("GitHub 更新服务返回 HTTP %d", resp.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024+1))
	if err != nil || len(data) > 512*1024 {
		return Notice{Err: errors.New("更新信息超出安全大小限制")}
	}
	var releases []Release
	if err := json.Unmarshal(data, &releases); err != nil {
		return Notice{Err: errors.New("GitHub 发布信息格式不正确")}
	}
	return Select(installed, releases)
}

func Check(installed string) Notice {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	client := &http.Client{
		Timeout:       checkTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return Fetch(ctx, installed, client)
}
