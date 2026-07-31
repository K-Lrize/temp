package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot 是本仓库自己的配置树。internal/config -> 上两级。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestRepositoryConfigIsValid 是配置树本身的门禁：任何一次改 lines/ devices/
// sets/ 的提交，只要配置写错就在这里失败，不必等到 CI 上跑构建。
func TestRepositoryConfigIsValid(t *testing.T) {
	cfg, ps, err := Load(repoRoot(t))
	if err != nil {
		t.Fatalf("载入本仓库配置树失败: %v", err)
	}
	if ps.HasError() {
		t.Fatalf("本仓库的配置树有错：\n%s", ps)
	}
	if len(ps) > 0 {
		t.Logf("非阻断的提示：\n%s", ps)
	}
	if len(cfg.Devices) == 0 {
		t.Fatal("一台设备都没载入，配置树布局大概率不对")
	}
}

// TestMigrationPreservesPackageSets 是从 wrt-build 迁过来时唯一有意义的
// 等价性检查：把 124 个硬列在一处的包拆进 11 个包集之后，每台设备最终
// 装的东西必须与拆之前一模一样。
//
// 比的是集合而不是顺序——包集重排本来就会改变顺序，而 ImageBuilder 的
// PACKAGES 是无序的。基线取自旧仓库的 devices/*/expected/resolved.json。
func TestMigrationPreservesPackageSets(t *testing.T) {
	cfg, _, err := Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range cfg.SortedDeviceNames() {
		t.Run(name, func(t *testing.T) {
			baseline := readBaseline(t, name)
			device := cfg.Devices[name]

			layers := make([]Layer, 0, len(device.Packages.Include)+1)
			for _, setName := range device.Packages.Include {
				set, ok := cfg.Sets[setName]
				if !ok {
					t.Fatalf("include 的包集 %q 不存在", setName)
				}
				layers = append(layers, Layer{
					Name: "set:" + setName,
					Spec: PackageSpec{Add: set.Add, Remove: set.Remove},
				})
			}
			layers = append(layers, Layer{Name: "device:" + name, Spec: device.Packages})

			merged, ps := MergePackages(layers)
			if ps.HasError() {
				t.Fatalf("合并出错：\n%s", ps)
			}

			got := merged.List()
			sort.Strings(got)
			if diff := diffSets(baseline, got); diff != "" {
				t.Fatalf("与迁移前的包列表不一致：\n%s", diff)
			}
		})
	}
}

func readBaseline(t *testing.T, device string) []string {
	t.Helper()
	path := filepath.Join("testdata", "migration", device+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移基线 %s: %v（新增设备时请一并补基线，或把这台设备从基线检查中排除）", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	sort.Strings(lines)
	return lines
}

func diffSets(want, got []string) string {
	inWant := map[string]bool{}
	for _, s := range want {
		inWant[s] = true
	}
	inGot := map[string]bool{}
	for _, s := range got {
		inGot[s] = true
	}

	var b strings.Builder
	for _, s := range want {
		if !inGot[s] {
			b.WriteString("  丢了: " + s + "\n")
		}
	}
	for _, s := range got {
		if !inWant[s] {
			b.WriteString("  多了: " + s + "\n")
		}
	}
	return b.String()
}
