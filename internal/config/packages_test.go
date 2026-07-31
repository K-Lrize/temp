package config

import (
	"reflect"
	"strings"
	"testing"
)

func layer(name string, add, remove []string) Layer {
	return Layer{Name: name, Spec: PackageSpec{Add: add, Remove: remove}}
}

func TestMergePackages(t *testing.T) {
	tests := []struct {
		name   string
		layers []Layer
		add    []string
		remove []string
		rules  []string
	}{
		{
			name:   "空层列表",
			layers: nil,
		},
		{
			name:   "单层直通",
			layers: []Layer{layer("device:vm", []string{"luci", "qemu-ga"}, nil)},
			add:    []string{"luci", "qemu-ga"},
		},
		{
			name: "层序决定最终顺序：先 set 后 device",
			layers: []Layer{
				layer("set:common", []string{"curl", "jq"}, nil),
				layer("set:base-router", []string{"ip-full"}, nil),
				layer("device:mt3600be", []string{"sing-box"}, nil),
			},
			add: []string{"curl", "jq", "ip-full", "sing-box"},
		},
		{
			// 去重保留首次出现的位置：包列表顺序会进指纹，靠后去重会让
			// 「往某个 set 里加一个别处已有的包」凭空改变全设备指纹。
			name: "跨层重复的包只保留首次出现的位置",
			layers: []Layer{
				layer("set:a", []string{"curl", "jq"}, nil),
				layer("set:b", []string{"htop", "curl"}, nil),
			},
			add: []string{"curl", "jq", "htop"},
		},
		{
			name: "同层内部重复也去重",
			layers: []Layer{
				layer("set:a", []string{"curl", "curl", "jq"}, nil),
			},
			add: []string{"curl", "jq"},
		},
		{
			name: "remove 取并集并去重",
			layers: []Layer{
				layer("set:base-router", nil, []string{"dnsmasq", "wpad-basic"}),
				layer("device:mt3600be", nil, []string{"wpad-basic", "wpad-mbedtls"}),
			},
			remove: []string{"dnsmasq", "wpad-basic", "wpad-mbedtls"},
		},
		{
			name: "add 与 remove 分别成列，互不干扰",
			layers: []Layer{
				layer("set:base-router", []string{"dnsmasq-full"}, []string{"dnsmasq"}),
				layer("device:mt3600be", []string{"sing-box"}, nil),
			},
			add:    []string{"dnsmasq-full", "sing-box"},
			remove: []string{"dnsmasq"},
		},

		// 跨层冲突：不做「后面的层覆盖前面的」。那种规则在五层之后没人能
		// 预测结果，而这里赌错的代价是设备上少一个包或多一个包，要刷机才发现。
		{
			name: "set 装的包被 device 卸掉 —— 冲突",
			layers: []Layer{
				layer("set:base-router", []string{"dnsmasq-full"}, nil),
				layer("device:mt3600be", nil, []string{"dnsmasq-full"}),
			},
			add:    []string{"dnsmasq-full"},
			remove: []string{"dnsmasq-full"},
			rules:  []string{"packages.conflict"},
		},
		{
			name: "set 卸的包被 device 装回来 —— 同样是冲突，不是覆盖",
			layers: []Layer{
				layer("set:base-router", nil, []string{"dnsmasq"}),
				layer("device:mt3600be", []string{"dnsmasq"}, nil),
			},
			add:    []string{"dnsmasq"},
			remove: []string{"dnsmasq"},
			rules:  []string{"packages.conflict"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ps := MergePackages(tc.layers)

			if !equalStrings(got.Add, tc.add) {
				t.Errorf("add 不符\n want: %v\n got:  %v", tc.add, got.Add)
			}
			if !equalStrings(got.Remove, tc.remove) {
				t.Errorf("remove 不符\n want: %v\n got:  %v", tc.remove, got.Remove)
			}
			if rules := ps.Rules(); !reflect.DeepEqual(rules, tc.rules) {
				t.Errorf("规则不符\n want: %v\n got:  %v\n%s", tc.rules, rules, ps)
			}
		})
	}
}

func TestMergeConflictMessageNamesBothLayers(t *testing.T) {
	// 冲突报错必须点名是哪两层撞了：五层合并之后，只说「dnsmasq 冲突」
	// 等于让人挨个翻 sets/。
	_, ps := MergePackages([]Layer{
		layer("set:base-router", []string{"dnsmasq-full"}, nil),
		layer("device:mt3600be", nil, []string{"dnsmasq-full"}),
	})
	msg := ps.String()
	for _, want := range []string{"set:base-router", "device:mt3600be", "dnsmasq-full"} {
		if !strings.Contains(msg, want) {
			t.Errorf("冲突信息里缺少 %q：\n%s", want, msg)
		}
	}
}

func TestPackagesList(t *testing.T) {
	// ImageBuilder 的 `make image PACKAGES=` 语法：装的包直接写，卸的包带 -
	// 前缀。remove 一律排在最后，与上一代产出的清单逐字节一致。
	p := Packages{
		Add:    []string{"luci", "sing-box"},
		Remove: []string{"dnsmasq", "wpad-basic"},
	}
	want := []string{"luci", "sing-box", "-dnsmasq", "-wpad-basic"}
	if got := p.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestPackagesListEmpty(t *testing.T) {
	if got := (Packages{}).List(); len(got) != 0 {
		t.Fatalf("空包集应产出空列表，得到 %v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
