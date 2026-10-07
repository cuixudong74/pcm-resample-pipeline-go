# pcm-resample-pipeline-go

纯 Go、无外部服务依赖的流式 PCM 格式转换与重采样库。它允许调用方按任意大小的字节块持续喂入交错 PCM（interleaved PCM），并在得到足够样本时返回转换结果；输出块不需要与输入块对齐，但所有分块方式产生的完整输出逐字节一致。

## 快速开始

```go
c, err := pcm.NewConverter(
    pcm.PCMFormat{
        SampleRate:   48000,
        Channels:     2,
        SampleFormat: pcm.FormatFloat32LE,
        Layout:       pcm.LayoutStereo,
    },
    pcm.PCMFormat{
        SampleRate:   16000,
        Channels:     1,
        SampleFormat: pcm.FormatInt16LE,
        Layout:       pcm.LayoutMono,
    },
    pcm.WithSincResampling(16, 0),
    pcm.WithDither(pcm.DitherTPDF, 1234),
)
if err != nil {
    return err
}

s := c.NewStream()
var result []byte

for chunk := range inputChunks { // chunk 可以是任意长度
    out, err := s.Append(chunk)
    result = append(result, out...)
    if err != nil {
        // err 是 *pcm.StreamError；该流已失败，可另建 NewStream 重试。
        return err
    }
}
tail, err := s.Close()
result = append(result, tail...)
```

`Append(nil)` 是合法的，只表示当前没有新输入。`Close` 会刷新重采样器保留的尾部样本。

## 支持的样本表示

所有样本均为紧凑排列、无样本间 padding：

| 表示 | 格式常量 | 内部归一化范围 |
| --- | --- | --- |
| 8-bit 无符号整数 | `FormatUint8` | `128` 为零，`[0,255]` |
| 16-bit 有符号整数 | `FormatInt16LE` / `FormatInt16BE` | `[-1,+1]` |
| 24-bit 有符号整数 | `FormatInt24LE` / `FormatInt24BE` | `[-1,+1]` |
| 32-bit 有符号整数 | `FormatInt32LE` / `FormatInt32BE` | `[-1,+1]` |
| 32-bit 浮点 | `FormatFloat32LE` / `FormatFloat32BE` | IEEE-754，有限值；合法输入要求 `[-1,+1]` |
| 64-bit 浮点 | `FormatFloat64LE` / `FormatFloat64BE` | IEEE-754，有限值；合法输入要求 `[-1,+1]` |

整数使用二补码。正满幅和负满幅都可往返转换；浮点输入中的有限值超出 `[-1,+1]` 会被拒绝，`NaN`/`±Inf` 被归类为损坏数据。转换内部使用 float64，重采样可能产生有限的轻微振铃：浮点输出保留这些有限值，整数输出按目标码型饱和。

支持的声道布局：

- `LayoutMono`：`C`
- `LayoutStereo`：`L,R`
- `Layout51`：`L,R,C,LFE,BL,BR`
- `Layout71`：`L,R,C,LFE,BL,BR,SL,SR`

不支持的采样率（必须在 `[1,1000000]` Hz）、位深、声道数或与声道数不匹配的布局会在 `NewConverter` 中明确拒绝，不会生成错误样本。

## 流式语义与确定性

- 每个 `NewStream` 是独立状态：字节偏移、滤波器历史、尾部缓存、抖动随机数均不共享。
- 输入块可以是任意长度；不完整帧会跨 `Append` 边界保留，不补零、不截断。
- 重采样器只滑动保留仍会被后续滤波器触达的样本，不需要等待完整文件。
- 同一转换器配置、同一输入和相同分块序列得到相同结果；同一 TPDF 种子在任意分块下也得到相同完整输出。
- 单条流失败后不能继续使用；失败不会影响由同一 `Converter` 创建的其他流。

## 重采样

默认配置为 Hann 窗 sinc FIR：

```go
pcm.WithSincResampling(halfWidth, cutoff)
```

- `halfWidth`：输入时间轴上每侧的支撑宽度（输入帧）。默认 `32`；允许 `[1,1024]`。增大会提高频率选择性，但也增加约 `halfWidth` 输入帧的正常输出延迟。
- `cutoff`：低通截止系数，范围 `(0,1]`；传 `0` 自动使用 `min(1, inRate/outRate)`。下采样时自动抗混叠。
- 窗函数：Hann 窗。
- 边界：首尾均采用确定的零延拓。流开始前和结束后的输入都视为零。
- 长度：输出严格为

```text
ceil(inputFrames * outRate / inRate)
```

因此最后的真实输入样本不会被丢弃，也不会重复。

低延迟/低质量模式可使用：

```go
pcm.WithLinearResampling()
```

线性模式使用两点线性插值，正常算法延迟约 1 个输入帧；它不提供强抗混叠，适用于质量要求低或比率温和的场景。相同输入/输出采样率会自动直通，不引入滤波延迟。

为保证精确整数相位运算不溢出，单条流的帧数上限为 `2^33`。在最高支持采样率 1 MHz 下仍可容纳约 107 天的数据；达到该上限会返回 `ErrorInvalidConfig`，不会静默错算。

## 位深转换与抖动

默认整数向下转换使用确定性四舍五入，不加噪。可启用 TPDF：

```go
pcm.WithDither(pcm.DitherTPDF, seed)
```

- TPDF 仅允许在“有效整数精度降低”时使用，例如 f64→s16、s24→s16。
- 整数位深向上转换、同位深转换、任意浮点输出均拒绝 TPDF，因为这些情况不应额外引入噪声。
- TPDF 由两个独立均匀数相减得到，幅度为目标整数的一个 LSB。
- PRNG 是确定性的 64-bit PCG；同一 `Seed` 可复现，抖动样本按交错输出顺序消耗，与输入如何分块无关。
- 抖动后超过目标整数范围的值饱和到最近满幅码，不回绕。

## 声道上混/下混规则

声道转换使用固定线性矩阵，不做动态增益控制。

- 相同声道：直通。
- Mono→Stereo：`L=R=C/sqrt(2)`，保证功率近似一致且总幅度不超过 1。
- Stereo→Mono：`C=(L+R)/2`。
- Mono→5.1/7.1：只写 `C`，其他声道静音。
- 5.1/7.1→Stereo：
  - 同侧主声道系数为 1；
  - 同侧后环绕/侧环绕和 `C` 系数为 `1/sqrt(2)`；
  - `LFE` 不参与；
  - 每个输出行再按系数绝对值和归一化，避免多个满幅声道相加时溢出。
- 多声道→Mono：所有非 LFE 声道等权混合，`LFE` 排除，再按行归一化。
- 7.1→5.1：相同名称声道直通；`SL/SR` 以 50%/50% 折叠到 `BL/BR`，不丢弃环绕内容。
- 5.1→7.1：`BL/BR` 直通到同名输出，`SL/SR` 静音。

## 错误定位与类型

错误可断言为 `*pcm.StreamError`，也可用 `pcm.ErrorKindOf(err)` 获得稳定类型：

| Kind | 含义 |
| --- | --- |
| `ErrorUnsupportedFormat` | 格式、位深、字节序或声道布局不受支持 |
| `ErrorInvalidConfig` | 重采样或抖动配置非法，或要求了不允许的抖动 |
| `ErrorCorruptData` | 输入中出现 `NaN`/`Inf` 等损坏样本 |
| `ErrorValueOutOfRange` | 有限浮点输入超出 `[-1,+1]` |
| `ErrorIncompleteFrame` | `Close` 时仍有不能构成完整帧的字节 |
| `ErrorStreamClosed` | 流已关闭或失败后再次使用 |

`Offset` 是相对于逻辑输入开头的字节位置，跨多次 `Append` 累计。`Frame` 是从 0 开始的帧号；无法确定帧时为 `-1`。不完整帧不会被静默补零或截断。

## 测试

```sh
go test ./...
go test -race ./...
go vet ./...
```

测试覆盖：

- 任意分块（1 字节、非帧对齐、大块等）的逐字节一致性；
- 上/下采样、非整数比率、输出长度和首尾刷新；
- 位深转换、TPDF 可复现性和非法加噪拒绝；
- Mono/Stereo/5.1/7.1 的上混、下混、LFE 与静音规则；
- 损坏样本、有限越界值、不完整帧、非法格式/配置的可区分错误；
- 位深、采样率、声道同时变化的端到端转换。
