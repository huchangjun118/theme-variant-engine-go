# Theme Variant Engine

进程内的主题化组件样式解析引擎。组件描述（插槽 + 变体）与主题描述（令牌 +
分层覆盖）是两份独立输入，引擎把它们与每次调用的实例输入合并、展开引用，
产出**确定且可溯源**的解析结果。不依赖任何外部服务。

- 同一份组件描述 + 同一个主题版本 + 同一份实例输入，任何时候解析结果一致。
- 每个叶子样式值都记录来源层、主题名、变体名，可定位“是哪一层覆盖的”。
- 错误一律**整体拒绝**，不会返回半截结果。

## 安装与快速上手

```go
import theme "github.com/huchangjun118/theme-variant-engine-go/pkg/theme"

reg := theme.NewRegistry(theme.WithCacheSize(128))
err := reg.Publish(ctx, []*theme.Theme{myTheme}, myTheme.Name)

eng := theme.NewEngine(reg)
res, err := eng.Resolve(ctx, component,
    theme.InstanceInput{Variants: []string{"primary"}, Slots: ...},
    theme.ResolveOptions{},
)
```

可运行示例见 [`examples/basic`](examples/basic/main.go)。

## 模型

### 插槽（Slot）

组件声明若干具名插槽，每个插槽有自己的默认样式：

```go
Slots: []theme.SlotDecl{
    {Name: "root",  Default: theme.StyleMap{"color": "#111"}},
    {Name: "label", Default: theme.StyleMap{"weight": "normal"}},
}
```

### 变体（Variant）

组件声明若干具名变体。一个变体可以给任意插槽提供样式；多个变体可组合
使用，后生效的变体覆盖先生效的。

```go
Variants: []theme.VariantDecl{
    {Name: "primary", Slots: map[string]theme.StyleMap{
        "root":  {"color": "blue"},
        "label": {"weight": "bold"},
    }},
}
```

变体生效顺序默认按**组件描述中的声明顺序**（与请求时传入的顺序无关，因此
`["a","b"]` 与 `["b","a"]` 结果相同）。`ResolveOptions.VariantOrder` 可
显式固定顺序，此时它必须与请求集合完全一致，否则返回 `ErrInvalidDescriptor`。

引用未声明的变体键会返回 `*theme.VariantError`（可 `errors.Is(err,
theme.ErrUndeclaredVariant)`），错误信息同时指出**插槽名与变体名**。主题
里出现组件未声明的变体覆盖同样被拒绝。

## 分层来源与覆盖优先级

每个插槽按以下固定顺序叠加，编号越大优先级越高；同层内按字段级合并：

1. `base` —— 组件插槽默认样式
2. `base-variant` —— 组件声明的变体，按变体生效顺序
3. `theme` —— 主题对该插槽的基础覆盖，沿继承链**父先子后**
4. `theme-variant` —— 主题对该插槽的变体覆盖，父先子后、再按变体顺序
5. `inherit` —— 外层组件上下文（`ResolveOptions.OuterContext`）
6. `instance` —— 调用 `Resolve` 时直接传入的插槽样式，永远最高优先

因此：实例值 > 继承值 > 主题值 > 基础值；子主题值 > 父主题值。

### 字段级合并 vs 整体替换

- **字段级合并（默认）**：嵌套 map 逐键递归合并，标量 / 布尔 / null /
  数组在叶子键上整体替换。例如基础 `padding:{x:1,y:2}` 被主题
  `padding:{x:8}` 覆盖后为 `padding:{x:8,y:2}`。
- **整体替换**：主题层的 `SlotOverride{Replace: true, Style: ...}` 会丢弃
  此前所有更低优先级层（含基础与父主题）对该插槽的累积，从该层重新开始；
  更高优先级层（后续子主题、继承、实例）仍会继续叠加。

### 来源定位

解析结果的每个插槽都带有 `Source map[string]Source`，键为点分叶子路径：

```go
src, ok := res.ProvenanceFor("root", "color")
// src.Layer == theme.LayerInstance / LayerTheme / ...
// src.Theme, src.Variant, src.Slot
```

引用派生值的 `Source.Resolved` 会继续指向其引用目标的来源。

## 主题继承与嵌套

- `Theme.Inherits` 声明父主题；链在 `Publish` 时解析，**父先子后**合并，
  子主题同名字段胜出（令牌与组件覆盖都如此）。
- 子组件通过 `OuterContext` 默认继承外层上下文；实例传入值优先于继承值。
- 继承链**循环**返回 `*InheritanceError{Cycle: true}`（`errors.Is(...,
  ErrInheritanceCycle)`）；引用了不存在的父主题是**断链**，返回
  `*InheritanceError{Cycle: false}`（`ErrInheritanceBroken`），二者可区分，
  且都不会静默退回默认值——整个发布被拒绝。

## 引用与派生

样式字符串里可以引用令牌或其它样式键：

| 写法 | 含义 |
| --- | --- |
| `{token:path.to.token}` | 引用主题令牌（沿继承链合并后的令牌表） |
| `{ref:slot.key.path}` | 引用其它插槽的叶子样式键 |
| `{ref:.key.path}` | 引用当前插槽自己的键（前导点） |

- 整个字符串恰好是一个引用时，保留目标值的**原生类型**（数字仍是数字）。
- 引用嵌入在更长字符串中时，按字符串替换（如 `"color: {token:c}"`）。
- 引用未定义目标、指向非叶子子树、或形成循环时，返回
  `*ReferenceError`（`ErrReferenceUndefined` / `ErrReferenceCycle`），并带
  插槽、键、引用路径；**整个解析被拒绝**，不产出任何半截结果。
- 令牌值本身也可以再引用令牌；循环同样被识别。

## 版本化热替换与并发

- `Registry.Publish(ctx, themes, defaultName)` 先在锁外完成全部校验（规范
  版本、重复名、每条继承链），通过后用**一次指针切换**安装不可变快照。
- `Resolve` 开始时固定一个快照，单次解析绝不会读到新旧混合的主题。
- 慢解析不持有发布锁，不会阻塞替换。
- `Publish` 在切换前检查 `ctx.Err()`；被取消或校验失败的发布不会改变当前
  生效主题，`Epoch()` 不递增。
- `ResolvedComponent.ThemeVersion` 记录本次结果对应的主题版本。

## 缓存与失效

- 缓存键仅由：组件描述、主题名、**主题版本**、实例输入（变体/插槽/外层
  上下文/变体顺序）以及内置 `AlgorithmVersion` 派生（SHA-256）。
- 主题热替换后 epoch 变化，旧版本条目不命中并被移除；升级算法版本（常量
  `AlgorithmVersion`）同样让旧缓存整体失效。
- 条目在写入前整体构建并附带校验和，读取时重算校验和；损坏或半截写入的
  条目永远不会被当成有效结果。
- 缓存是有界 LRU，`WithCacheSize(n)` 限制条目数，占用不随请求量无界增长；
  `0` 关闭缓存，默认 256。

## 规范版本兼容

- 支持范围：`MinSupportedSpec = "1.0"` 到 `MaxSupportedSpec = "1.4"`
  （按 major.minor 比较，边界包含）。超出范围或格式非法返回
  `*VersionError`（`errors.Is(..., ErrSpecVersion)`），组件在 `Resolve`
  时被拒绝，主题在 `Publish` 时被拒绝。
- 废弃字段 `ComponentDescriptor.DefaultStyles` 有**确定语义**，不会被静默
  忽略：当且仅当 `root` 插槽未声明自己的默认值时，作为 `root` 的默认
  （来源层记为 `base`）；同时产出 `Warning{Kind: "deprecated",
  Path: "defaultStyles"}`，警告会出现在 `ResolvedComponent.Warnings` 中。
- 用 `DecodeComponent(raw, strict)` / `DecodeTheme(raw, strict)` 从 JSON
  解码：`strict=true` 时未知字段是硬错误；非严格模式下未知字段产生
  `Warning{Kind: "unknown-field"}`，保证不被静默吞掉。

## 可配置项

| 配置 | 位置 | 说明 |
| --- | --- | --- |
| `WithCacheSize(n)` | `NewRegistry` | 解析结果缓存上限，默认 256，0 关闭 |
| `ResolveOptions.ThemeName` | `Resolve` | 指定主题，空串用发布时的默认主题 |
| `ResolveOptions.VariantOrder` | `Resolve` | 固定变体生效顺序，须与请求集合一致 |
| `ResolveOptions.OuterContext` / `OuterComponent` | `Resolve` | 外层继承的插槽样式与来源标识 |
| `SlotOverride.Replace` | 主题描述 | 该层对插槽整体替换而非字段合并 |
| `AlgorithmVersion` | 常量 | 算法版本，变化即缓存整体失效 |
| 支持的 spec 范围 | `MinSupportedSpec`/`MaxSupportedSpec` | 描述声明版本的接受区间 |

## 错误速查

| 场景 | 判定 |
| --- | --- |
| 未声明变体 | `errors.Is(err, ErrUndeclaredVariant)`，具体为 `*VariantError` |
| 未知插槽 | `ErrUnknownSlot`，具体为 `*SlotError` |
| 继承循环 / 断链 | `ErrInheritanceCycle` / `ErrInheritanceBroken`，具体为 `*InheritanceError` |
| 引用未定义 / 循环 | `ErrReferenceUndefined` / `ErrReferenceCycle`，具体为 `*ReferenceError` |
| spec 版本超范围 | `ErrSpecVersion`，具体为 `*VersionError` |
| 描述非法（重复名、空名、变体顺序不匹配等） | `ErrInvalidDescriptor` |

## 测试

```sh
go test ./...
go test -race ./...
```

覆盖：变体组合与确定性、六层覆盖优先级与溯源、字段合并与整体替换、继承
父链/循环/断链、引用未定义与循环（含令牌循环）、热替换并发一致性、发布
失败/取消不污染、缓存命中/有界/版本失效/损坏条目防护、规范版本与废弃字段
语义。
