package id

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildFormat(t *testing.T) {
	got, err := Build(at("2026-07-30T14:21:04Z"), "42", "f0a60ee")
	if err != nil {
		t.Fatal(err)
	}
	if want := "20260730-142104-42-f0a60ee"; got != want {
		t.Errorf("build_id = %q, want %q", got, want)
	}
}

func TestReleaseIsBuildWithRPrefix(t *testing.T) {
	now := at("2026-07-30T14:21:04Z")
	b, _ := Build(now, "42", "f0a60ee")
	r, err := Release(now, "42", "f0a60ee")
	if err != nil {
		t.Fatal(err)
	}
	if r != "r"+b {
		t.Errorf("release_id = %q, want %q", r, "r"+b)
	}
}

func TestTimeIsNormalisedToUTC(t *testing.T) {
	// 注入一个带时区的时间，输出必须是 UTC——否则不同机器的本地时区会算出
	// 不同的 id。
	east := time.FixedZone("UTC+8", 8*3600)
	got, err := Build(time.Date(2026, 7, 30, 22, 21, 4, 0, east), "42", "f0a60ee")
	if err != nil {
		t.Fatal(err)
	}
	if want := "20260730-142104-42-f0a60ee"; got != want {
		t.Errorf("带时区时间没有归一到 UTC：%q, want %q", got, want)
	}
}

func TestEmptyPartsRejected(t *testing.T) {
	now := at("2026-07-30T14:21:04Z")
	for _, tc := range []struct{ name, run, sha string }{
		{"run 空", "", "f0a60ee"},
		{"sha 空", "42", ""},
		{"run 全空白", "  ", "f0a60ee"},
		{"两个都空", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Build(now, tc.run, tc.sha); err == nil {
				t.Errorf("run=%q sha=%q 应当报错", tc.run, tc.sha)
			}
		})
	}
}

func TestLexicalOrderMatchesTimeOrder(t *testing.T) {
	// 字典序即时间序：这是 GC / 列举时"最新在最后"能不靠额外字段成立的前提。
	earlier, _ := Build(at("2026-07-30T14:21:04Z"), "42", "f0a60ee")
	later, _ := Build(at("2026-07-30T14:21:05Z"), "42", "f0a60ee")
	if !(earlier < later) {
		t.Errorf("字典序应与时间序一致：%q 应当 < %q", earlier, later)
	}
}

func TestShort(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"f0a60eee2fe051741c643ea6118718aae1ef17fb", "f0a60ee"},
		{"f0a60ee", "f0a60ee"},
		{"abc", "abc"},
		{"  f0a60eee  ", "f0a60ee"},
	} {
		if got := Short(tc.in); got != tc.want {
			t.Errorf("Short(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
