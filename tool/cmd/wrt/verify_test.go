package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyManifestFailsOnDisappearance(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "prev.manifest", "a 1.0\nb 1.0\nc 1.0\n")
	write(t, dir, "curr.manifest", "a 2.0\nc 1.0\n") // b 掉了
	prev := filepath.Join(dir, "prev.manifest")
	curr := filepath.Join(dir, "curr.manifest")

	stdout, _, err := runCLI(t, atRepo(t, "verify", "manifest", "--prev", prev, "--curr", curr)...)
	if err == nil {
		t.Fatal("有包消失时 verify manifest 必须返回非零")
	}
	if !strings.Contains(stdout, "消失: b") {
		t.Errorf("应指名消失的包 b：\n%s", stdout)
	}
}

func TestVerifyManifestPassesWhenNoPrev(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "curr.manifest", "a 1.0\n")
	curr := filepath.Join(dir, "curr.manifest")

	// 无 --prev：首个 release 没有基线，放行。
	if _, stderr, err := runCLI(t, atRepo(t, "verify", "manifest", "--curr", curr)...); err != nil {
		t.Fatalf("无基线时应放行：%v\n%s", err, stderr)
	}
}
