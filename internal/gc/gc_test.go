package gc

import (
	"reflect"
	"testing"
)

func TestTopN(t *testing.T) {
	ids := []string{
		"20260701-100000-1-aaa",
		"20260703-100000-3-ccc",
		"20260702-100000-2-bbb",
	}
	got := TopN(ids, 2)
	want := []string{"20260703-100000-3-ccc", "20260702-100000-2-bbb"} // 最新在前
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopN = %v, want %v", got, want)
	}
	// n 大于总数时不越界
	if got := TopN(ids, 10); len(got) != 3 {
		t.Errorf("n 超过总数应返回全部: %v", got)
	}
}

func TestLiveReleaseIDsUnionsPinnedThatExist(t *testing.T) {
	all := []string{"r-1", "r-2", "r-3", "r-4"} // 字典序即时间序
	// 保留最新 1 个（r-4），外加 pin 的 r-1；r-99 不存在，忽略。
	got := LiveReleaseIDs(all, 1, []string{"r-1", "r-99"})
	want := []string{"r-1", "r-4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LiveReleaseIDs = %v, want %v", got, want)
	}
}

func TestLiveReleaseIDsPinnedNonexistentIgnored(t *testing.T) {
	// 只 pin 一个不存在的 id，不该报错、也不该凭空造出存活项。
	got := LiveReleaseIDs([]string{"r-1"}, 0, []string{"nope"})
	if len(got) != 0 {
		t.Errorf("失效 pin 应被忽略，得到 %v", got)
	}
}

func TestClassify(t *testing.T) {
	existing := []Entry{
		{Key: "b-1", Path: "line/targets/t/s/builds/b-1"},
		{Key: "b-2", Path: "line/targets/t/s/builds/b-2"},
		{Key: "b-3", Path: "line/targets/t/s/builds/b-3"},
	}
	keep, del := Classify(existing, []string{"b-2"})
	if !reflect.DeepEqual(keep, []string{"line/targets/t/s/builds/b-2"}) {
		t.Errorf("keep = %v", keep)
	}
	if !reflect.DeepEqual(del, []string{"line/targets/t/s/builds/b-1", "line/targets/t/s/builds/b-3"}) {
		t.Errorf("delete = %v", del)
	}
}

func TestOverThreshold(t *testing.T) {
	for _, tc := range []struct {
		total, del, pct int
		want            bool
	}{
		{total: 0, del: 0, pct: 30, want: false},   // 无对象，安全
		{total: 10, del: 3, pct: 30, want: false},  // 正好 30%，不超过
		{total: 10, del: 4, pct: 30, want: true},   // 40% > 30%
		{total: 100, del: 30, pct: 30, want: false},
		{total: 100, del: 31, pct: 30, want: true},
	} {
		if got := OverThreshold(tc.total, tc.del, tc.pct); got != tc.want {
			t.Errorf("OverThreshold(%d,%d,%d) = %v, want %v", tc.total, tc.del, tc.pct, got, tc.want)
		}
	}
}
