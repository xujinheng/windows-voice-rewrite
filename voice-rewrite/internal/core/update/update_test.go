package update

import (
	"strings"
	"testing"
)

func TestVersionOrdering(t *testing.T) {
	for _, item := range []struct {
		a, b string
		want int
	}{
		{"0.25.0", "0.24.9", 1},
		{"v0.25.0", "0.25.0", 0},
		{"0.25.0-dev", "0.25.0", -1},
		{"0.25.0-rc.10", "0.25.0-rc.9", 1},
		{"1.0.0-alpha.1", "1.0.0-alpha", 1},
		{"1.0.0+build.1", "v1.0.0", 0},
	} {
		a, ok := Parse(item.a)
		if !ok {
			t.Fatalf("parse %s", item.a)
		}
		b, ok := Parse(item.b)
		if !ok {
			t.Fatalf("parse %s", item.b)
		}
		if got := Compare(a, b); got != item.want {
			t.Errorf("%s vs %s = %d, want %d", item.a, item.b, got, item.want)
		}
	}
}

func TestUpdateSelection(t *testing.T) {
	releases := []Release{
		{TagName: "v0.24.0"}, {TagName: "v0.30.0", Draft: true},
		{TagName: "v0.26.0-beta.2", Prerelease: true}, {TagName: "v0.25.0"},
	}
	notice := Select("0.25.0-dev", releases)
	if notice.Err != nil || !notice.Found || !notice.Newer || notice.Latest != "v0.26.0-beta.2" {
		t.Fatalf("result: %#v", notice)
	}
	if !strings.Contains(notice.Summary("0.25.0-dev"), "发现新版本 v0.26.0-beta.2") || strings.Contains(Prompt(notice.Latest), "自动下载或运行未经签名") == false {
		t.Fatal("update prompt changed")
	}
	current := Select("v0.26.0-beta.2", releases)
	if current.Newer || !strings.Contains(current.Summary("v0.26.0-beta.2"), "已是最新版本") {
		t.Fatalf("current release: %#v", current)
	}
	if !strings.Contains(Select("0.1.0", nil).Summary("0.1.0"), "尚未发布") {
		t.Fatal("missing releases summary")
	}
}

func TestReleaseSourceMatchesVoiceRewriteRepository(t *testing.T) {
	if ReleasesAPI != "https://api.github.com/repos/xujinheng/windows-voice-rewrite/releases?per_page=15" {
		t.Fatalf("unexpected update source: %s", ReleasesAPI)
	}
	if ReleasesPage != "https://github.com/xujinheng/windows-voice-rewrite/releases" {
		t.Fatalf("unexpected release page: %s", ReleasesPage)
	}
}
