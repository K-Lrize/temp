// Package config 是配置的唯一真相：类型定义即 schema，校验只在这里发生一次。
//
// 上一代实现把同一批约束写了三遍（手写的 JSON Schema、bash 里的断言、lint 脚本
// 里的语义规则），三处必然漂移，最后没人知道以哪个为准。这里的规矩是：
// 任何一条配置约束，只允许在本包内表达一次；`wrt schema export` 导出的 JSON
// Schema 是给编辑器补全用的生成物，不是第二份真相。
package config

// Artifacts 决定 L2（SDK/IB + kmod + target base 包）整层的来源。
//
// 上一代把它拆成 mode + kmod_source 两个字段，但四格矩阵里只有对角线能工作：
//   - official 底座 + self kmod：自建 kmod 路径在官方线下压根不存在，设备 apk update 404
//   - self 底座 + official kmod：官方 kmod 按官方那次编译的 vermagic 键控，
//     自建内核的 vermagic 含配置哈希，对得上只能靠运气；固件能刷，驱动装不上
//
// 两格都只在刷机之后才暴露。既然只有对角线可用，它本来就是一个布尔值。
type Artifacts string

const (
	// ArtifactsOfficial：SDK/IB、kmod、target base 包全部借 OpenWrt 官方发布产物。
	ArtifactsOfficial Artifacts = "official"
	// ArtifactsSelf：以上三者全部由本仓库的工具链流水线自行编译产出。
	ArtifactsSelf Artifacts = "self"
)

// Source 指向一棵 OpenWrt 源码树。
type Source struct {
	Repo string `yaml:"repo" json:"repo"`
	// Commit 是唯一权威。tag 理论上不可变，但只信 commit 才能保证
	// 「这次构建到底编了哪个源码状态」是可回答、可复现的。
	Commit string `yaml:"commit" json:"commit"`
	// Ref 供人读，CI 不用它检出。唯一的机器用途是核对 upstream 与源码
	// 是否属于同一条版本线（见 Line.Validate）。
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// Line 是一条源码基线 + 由它产出的产物身份。
//
// 不变量：两台设备共用一条 line，当且仅当它们可以安全共用同一份 SDK/IB、
// 同一份 kmod 仓、同一份用户态包仓库。
//
// 上游版本不同、源码 repo/commit 不同、artifacts 不同、有无 patch 不同
// —— 任一条成立就是两条不同的 line。约束挂在 line 上而不是 device 上，
// 因为一个 device 无法约束另一个 device。
type Line struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description,omitempty"`
	// Upstream 是完整 patch 号（如 25.12.5），不允许只写 25.12 让系统猜。
	// L3 社区 feed 借的就是这条线。
	Upstream  string    `yaml:"upstream"`
	Artifacts Artifacts `yaml:"artifacts"`
	Source    *Source   `yaml:"source,omitempty"`

	// RequiresBuild 是派生结论：lines/<id>/ 下 overlay、patches、config 任一非空，
	// 即代表源码相对官方有实质修改。由载入层按磁盘事实填，不可从 YAML 写入
	// —— 否则就能手写一个与磁盘不符的值。
	RequiresBuild bool `yaml:"-"`
}

// Hardware 是一台设备的构建坐标。
type Hardware struct {
	Target    string `yaml:"target" json:"target"`
	Subtarget string `yaml:"subtarget" json:"subtarget"`
	Profile   string `yaml:"profile" json:"profile"`
	Arch      string `yaml:"arch" json:"arch"`
}

// TargetKey 是 target/subtarget 组合，同时也是 R2 上 targets/ 下的路径片段。
func (h Hardware) TargetKey() string { return h.Target + "/" + h.Subtarget }

// PackageSpec 是一层包清单。设备与包集共用同一种形状，因为合并算法把它们
// 一视同仁地当作有序层。
type PackageSpec struct {
	// Include 引用 sets/<name>.yaml，顺序有意义（决定最终包列表的顺序）。
	Include []string `yaml:"include,omitempty"`
	Add     []string `yaml:"add,omitempty"`
	Remove  []string `yaml:"remove,omitempty"`
}

type Image struct {
	RootfsPartsize int `yaml:"rootfs_partsize,omitempty" json:"rootfs_partsize"`
}

// Device 只描述硬件事实与装什么包，一个源码字段都没有。
// 切换版本线 = 改 Lines 里的一项。
type Device struct {
	Name     string   `yaml:"name"`
	Hardware Hardware `yaml:"hardware"`
	// Metadata 是事实性硬件资料（soc/wifi/owner/location），不参与构建逻辑。
	Metadata map[string]string `yaml:"metadata,omitempty"`
	// Lines 是这台设备的出货矩阵：每一项与设备展开成一个 variant。
	Lines    []string    `yaml:"lines"`
	Packages PackageSpec `yaml:"packages"`
	Image    Image       `yaml:"image,omitempty"`
	// Repos 是额外的第三方 apk 源，原样追加到三层 repositories 之后。
	Repos []string `yaml:"repos,omitempty"`
}

// Set 是可复用的包清单。不支持嵌套 include —— 合并函数接受有序层列表，
// 将来真需要多一层是配置变更而不是代码变更。
type Set struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Add         []string `yaml:"add,omitempty"`
	Remove      []string `yaml:"remove,omitempty"`
}
