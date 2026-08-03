package diag

import (
	"reflect"
	"strings"
	"testing"
)

func TestErrorfAndWarnfAccumulate(t *testing.T) {
	// 累积而不是遇错即返回：配置类问题成批出现，一次只报一条会让人来回
	// 跑五遍 lint。
	var ps Problems
	ps = ps.Errorf("a.rule", "坏了 %d 个", 2)
	ps = ps.Warnf("b.rule", "留意一下")
	ps = ps.Errorf("a.rule", "又坏了")

	if len(ps) != 3 {
		t.Fatalf("want 3 problems, got %d", len(ps))
	}
	if ps[0].Severity != SeverityError || ps[1].Severity != SeverityWarn {
		t.Fatalf("severity 不对: %+v", ps)
	}
	if ps[0].Message != "坏了 2 个" {
		t.Errorf("format 未展开: %q", ps[0].Message)
	}
}

func TestHasErrorIgnoresWarnings(t *testing.T) {
	var warnOnly Problems
	warnOnly = warnOnly.Warnf("w", "只是提示")
	if warnOnly.HasError() {
		t.Error("只有 warn 时 HasError 必须为 false，否则每条提示都会阻断构建")
	}

	if !warnOnly.Errorf("e", "真错了").HasError() {
		t.Error("有 error 时 HasError 必须为 true")
	}
	if (Problems)(nil).HasError() {
		t.Error("空集合不该报有错")
	}
}

func TestCount(t *testing.T) {
	var ps Problems
	ps = ps.Errorf("a", "1").Warnf("b", "2").Errorf("c", "3")
	if got := ps.Count(SeverityError); got != 2 {
		t.Errorf("error count = %d", got)
	}
	if got := ps.Count(SeverityWarn); got != 1 {
		t.Errorf("warn count = %d", got)
	}
}

func TestRulesAreDedupedAndSorted(t *testing.T) {
	// 测试断言「触发了哪几条规则」比断言具体措辞稳定得多，前提是这里
	// 的输出确定。
	var ps Problems
	ps = ps.Errorf("z.rule", "x").Errorf("a.rule", "y").Warnf("z.rule", "z")
	want := []string{"a.rule", "z.rule"}
	if got := ps.Rules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if got := (Problems)(nil).Rules(); got != nil {
		t.Errorf("空集合应返回 nil，得到 %v", got)
	}
}

func TestProblemsWithSourceDoesNotOverwriteExisting(t *testing.T) {
	ps := Problems{
		{Rule: "a", Message: "m"},
		{Rule: "b", Message: "m", Source: "already/set.yaml"},
	}
	got := ps.WithSource("lines/25.12/line.yaml")
	if got[0].Source != "lines/25.12/line.yaml" {
		t.Errorf("空 Source 应被回填，得到 %q", got[0].Source)
	}
	if got[1].Source != "already/set.yaml" {
		t.Errorf("已有 Source 不该被覆盖，得到 %q", got[1].Source)
	}
	if ps[0].Source != "" {
		t.Error("WithSource 不该原地修改入参")
	}
}

func TestStringIncludesSourceRuleAndSeverity(t *testing.T) {
	p := Problem{Source: "sets/x.yaml", Rule: "set.empty", Message: "空的", Severity: SeverityWarn}
	got := p.String()
	for _, want := range []string{"sets/x.yaml", "set.empty", "空的", "warn"} {
		if !strings.Contains(got, want) {
			t.Errorf("输出里缺少 %q: %s", want, got)
		}
	}

	// 出处未回填时也要能打印，不能输出一个空前缀让人无从下手。
	if got := (Problem{Rule: "r", Message: "m"}).String(); !strings.Contains(got, "unknown") {
		t.Errorf("缺少出处时应有占位: %s", got)
	}
}

func TestProblemsStringListsEveryLine(t *testing.T) {
	var ps Problems
	ps = ps.Errorf("a", "一").Warnf("b", "二")
	if got := strings.Count(ps.String(), "\n"); got != 2 {
		t.Errorf("每条问题一行，得到 %d 行", got)
	}
}

func TestSeverityString(t *testing.T) {
	if SeverityError.String() != "error" || SeverityWarn.String() != "warn" {
		t.Fatalf("severity 文案不对: %q %q", SeverityError, SeverityWarn)
	}
}
