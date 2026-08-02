package manifest

import (
	"reflect"
	"testing"
)

func TestNamesParsesBothFormatsDedupsAndSorts(t *testing.T) {
	// 混两种历史格式、带空行/注释/重复，只应取出排序去重后的包名。
	in := `
# 这是注释
luci-base 24.10
kmod-nft-core - 6.12.94-1
base-files 1500

luci-base 24.10
`
	got := Names(in)
	want := []string{"base-files", "kmod-nft-core", "luci-base"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v，期望 %v", got, want)
	}
}

func TestDisappearedFlagsOnlyMissingNames(t *testing.T) {
	prev := "a 1.0\nb 1.0\nc 1.0\n"
	// b 掉了；a 升级到 2.0（版本变化不算消失）；d 新增（不算）。
	curr := "a 2.0\nc 1.0\nd 1.0\n"
	got := Disappeared(prev, curr)
	want := []string{"b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Disappeared() = %v，期望 %v", got, want)
	}
}

func TestDisappearedEmptyWhenNothingLost(t *testing.T) {
	prev := "a 1.0\nb 1.0\n"
	curr := "b 2.0\na 1.1\nc 3.0\n"
	if got := Disappeared(prev, curr); len(got) != 0 {
		t.Fatalf("没有包消失时应为空，得到 %v", got)
	}
}
