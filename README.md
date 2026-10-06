# theme-variant-engine-go

主题化的组件样式解析引擎。组件的变体与插槽样式统一解析：同一份组件描述
配上同一份主题，任何时候解析结果一致；每个字段的取值都能定位到具体来源层。
全部过程在本地进程内完成，不依赖任何外部服务。

## 核心模型

- **插槽（Slot）**：组件的具名子部件，可声明默认样式。
- **变体（Variant）**：具名的样式覆盖集合，按插槽给出覆盖值；多个变体可
  组合使用，组合时按变体名排序依次叠加，结果与传入顺序无关。
- **主题（Theme）**：令牌（Tokens）+ 组件覆盖（ComponentOverride），
  可通过 `Parent` 形成继承链。
- **输入（Input）**：组件名 + 变体组合 + 实例级 `Props`（公共）/
  `SlotProps`（按插槽）。

## 合并语义

样式按字段递归合并：

| 两侧值的类型 | 语义 |
| --- | --- |
| 都是 map | 字段级递归合并 |
| 其他（标量、类型不一致、上层为 nil） | 上层整体替换下层 |

## 优先级顺序（低 → 高）

1. **base**：继承链展开后的插槽默认样式，再叠加组合变体的样式；
2. **theme**：主题继承链（父 → 子）合并后的组件覆盖，先槽位级、后变体级；
3. **instance**：实例传入，先 `Props`、后 `SlotProps`。

组件继承沿 `Extends` 链展开（根 → 子），子组件与父组件同名插槽/变体做
字段级合并；实例级取值始终优先于继承值。继承链循环报
`ErrInheritanceCycle`，父组件缺失报 `ErrBrokenInheritance`，两者可区分，
不会静默退回默认值。

解析结果的 `Slots[x].Origins` 记录每个字段路径的来源层
（`base` / `theme` / `instance`）与来源名（组件名、主题名、变体名等）。

## 引用与派生

字符串值支持两种引用：

- `{tokens.a.b}`：引用主题令牌（支持嵌套路径）；
- `{styles.k}`：引用同一插槽样式表中的其他键，实现派生。

未定义引用报 `ErrUndefinedReference`，循环引用报 `ErrReferenceCycle`；
任一失败都导致**整体解析失败**，不会产出半截结果。主题令牌内部的循环
在加载/热替换校验期即被拒绝。

## 热替换与并发

- `Engine` 内部持有原子快照指针，`Resolve` 每次读取的是自洽的单一版本，
  不会出现新旧混合；
- `ReloadTheme(ctx, theme)` 先完整校验新主题，校验失败或 `ctx` 被取消时
  不执行替换，当前生效主题不受污染；
- 替换是单次指针写入，慢解析不阻塞替换；替换成功后缓存整体失效。

## 缓存

- 缓存键由 **算法版本 + 主题版本 + 组件名 + 输入取值**（变体排序后）决定；
- 主题替换时 `flush` 整体失效；算法语义变化时递增 `algorithmVersion`
  使旧键自然失效；
- 每个条目带 FNV 校验和，损坏或部分写入的条目会被剔除并按未命中处理；
- LRU 有界（`New(..., cacheSize)`），超出上限淘汰最久未使用项，
  占用不随请求量无界增长。

## 规范版本兼容

- 组件与主题描述均可声明 `SpecVersion`，当前支持范围 **1.0 – 1.2**
  （主版本必须为 1，次版本 ≤ 2），越界报 `ErrUnsupportedSpecVersion`；
- 废弃字段 `ComponentDesc.DefaultVariants`：解析时仍生效，但会在
  `Result.DeprecatedWarnings` 中记录警告，不会被静默忽略。

## 可配置项

| 配置 | 位置 | 说明 |
| --- | --- | --- |
| `cacheSize` | `New(components, theme, cacheSize)` | 缓存条目上限，≤0 时默认 256 |
| `algorithmVersion` | `engine.go` 常量 | 解析算法版本，语义变化时递增 |
| 规范版本范围 | `spec.go` 常量 | `SupportedSpecMajor` / `SupportedSpecMaxMinor` |

## 错误类型一览

| 错误 | 场景 |
| --- | --- |
| `ErrUnknownComponent` | 解析或主题覆盖引用未注册组件 |
| `ErrUnknownVariant` | 引用未声明的变体键（含组件与变体名） |
| `ErrUnknownSlot` | 变体/覆盖引用未声明的插槽（含插槽与变体名） |
| `ErrInheritanceCycle` | 组件继承链循环 |
| `ErrBrokenInheritance` | 继承的父组件不存在 |
| `ErrUndefinedReference` | 引用未定义的令牌或样式键 |
| `ErrReferenceCycle` | 引用循环（样式间或令牌间） |
| `ErrUnsupportedSpecVersion` | 规范版本超出支持范围 |

## 测试

```sh
go test -race ./...
```

覆盖：变体组合确定性、覆盖优先级与来源定位、继承与循环/断链、
引用异常（未定义/循环/令牌循环）、热替换并发自洽性、取消与失败重载、
缓存失效/有界/损坏剔除、规范版本拒绝、废弃字段语义。
