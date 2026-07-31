package config

import (
	"fmt"
	"sort"
	"strings"
)

// Severity 区分「必须修」与「值得看一眼」。
//
// 分级原则：能确定地判定为错的才是 Error。判定依赖一张我们自己维护的收录表
// （如 arch↔target 对应关系）时降级为 Warn——为尚未接入的组合预先猜一个值，
// 猜错的代价比空着不查更高。
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarn
)

func (s Severity) String() string {
	if s == SeverityWarn {
		return "warn"
	}
	return "error"
}

// Problem 是一条校验发现。Rule 是稳定的规则代号，用于在文档与排障时互相指认；
// Source 由载入层回填（校验函数本身不知道自己是从哪个文件来的）。
type Problem struct {
	Source   string
	Rule     string
	Message  string
	Severity Severity
}

func (p Problem) String() string {
	src := p.Source
	if src == "" {
		src = "<unknown>"
	}
	return fmt.Sprintf("%s: [%s] %s (%s)", src, p.Severity, p.Message, p.Rule)
}

// Problems 是校验结果的累积容器。
//
// 校验一律走「累积后一次性返回全部问题」而不是「遇到第一个就返回 error」：
// 配置类错误往往成批出现，一次只报一条会让人来回跑五遍 lint。
type Problems []Problem

func (ps Problems) Errorf(rule, format string, args ...any) Problems {
	return append(ps, Problem{Rule: rule, Message: fmt.Sprintf(format, args...), Severity: SeverityError})
}

func (ps Problems) Warnf(rule, format string, args ...any) Problems {
	return append(ps, Problem{Rule: rule, Message: fmt.Sprintf(format, args...), Severity: SeverityWarn})
}

// WithSource 给一批还没有出处的问题回填来源文件。
func (ps Problems) WithSource(source string) Problems {
	out := make(Problems, len(ps))
	for i, p := range ps {
		if p.Source == "" {
			p.Source = source
		}
		out[i] = p
	}
	return out
}

// HasError 报告是否存在 Error 级问题。只有 Warn 时应当放行。
func (ps Problems) HasError() bool {
	for _, p := range ps {
		if p.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (ps Problems) Count(s Severity) int {
	n := 0
	for _, p := range ps {
		if p.Severity == s {
			n++
		}
	}
	return n
}

// Rules 列出结果里出现过的规则代号，去重且有序——测试断言「触发了哪几条规则」
// 比断言具体措辞稳定得多。
func (ps Problems) Rules() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range ps {
		if !seen[p.Rule] {
			seen[p.Rule] = true
			out = append(out, p.Rule)
		}
	}
	sort.Strings(out)
	return out
}

func (ps Problems) String() string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.String())
		b.WriteByte('\n')
	}
	return b.String()
}
