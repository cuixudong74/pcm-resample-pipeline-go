# pcm-resample-pipeline-go

纯本地、纯 Go 的流式音频格式转换与重采样库，不依赖任何外部服务。
输入可以是任意采样率 / 位深 / 声道布局 / 整数或浮点表示的交错 PCM
字节流，输出为统一格式的 PCM 字节流，支持边转换边消费。

## 快速开始

```go
in  := pcm.Format{SampleRate: 44100, BitDepth: 16, Channels: 2, SampleFormat: pcm.SampleFormatInt}
out := pcm.Format{SampleRate: 48000, BitDepth: 16, Channels: 2, SampleFormat: pcm.SampleFormatInt}

p, err := pcm.NewPipeline(in, out, pcm.Options{
    Quality:    pcm.QualityMedium,
    Dither:     pcm.DitherTPDF,
    DitherSeed: 42,
})
if err != nil { /* 不支持的格式组合，*pcm.Error，Kind == ErrKindUnsupported */ }

// 数据可以按任意大小的块喂入
for chunk := range chunks {
    if _, err := p.Write(chunk); err != nil { /* 见“错误语义” */ }
    n, _ := p.Read(buf) // 边喂边取，n == 0 表示暂时没有产出
}
p.Close() // 冲刷重采样器尾部；之后 Read 排空后返回 io.EOF
```

## 格式描述（Format）

| 字段 | 含义 | 合法值 |
|---|---|---|
| `SampleRate` | 每声道采样率 | > 0 |
| `BitDepth` | 每样本位深 | 整数：8/16/24/32；浮点：32/64 |
| `Channels` | 交错声道数 | 1–64 |
| `SampleFormat` | 样本表示 | `SampleFormatInt`（有符号补码）/ `SampleFormatFloat`（[-1,1] 归一化） |

样本按帧交错、小端存储。不支持的组合在 `NewPipeline` 阶段即以
`ErrKindUnsupported` 拒绝，不会悄悄产出错误样本。

## 转换语义

### 数值表示

- 整数深度 N 与浮点之间按满幅 `2^(N-1)` 线性映射：int16 `-32768 ↔ -1.0`，
  `32767 ↔ 32767/32768`。
- 浮点输入必须有限且在 `[-1, 1]` 内，否则报错（见“错误语义”）。
- 输出端一律钳制到目标格式可表示范围，不会回绕。

### 重采样

- 任意采样率互转，采用 Blackman 加窗 sinc 插值，抗混叠截止
  `fc = min(1, outRate/inRate)`。
- **质量 / 延迟**：`QualityLow` / `QualityMedium` / `QualityHigh` 分别对应
  每侧 8 / 16 / 32 个抽头；群延迟为 `halfTaps/fc` 个输入采样
  （下采样时按抽取比放大），质量越高延迟越大。
- **边界行为（确定）**：流之外的样本按零处理，且卷积核按可用样本的
  权重和重新归一化——直流信号在首尾边缘依然保持单位增益，接缝处
  无能量跳变、无咔哒声。
- **样本守恒**：N 帧输入最终恰好产出 `round(N*outRate/inRate)` 帧，
  末尾样本不丢弃、不重复。
- **分块无关**：输出只取决于输入样本序列，与 `Write` 的切块方式无关；
  同一输入无论切成多少块，输出逐字节一致（有测试保证）。

### 位深与抖动

- 仅在**降低精度**（浮点→整数，或整数位深变低）时按 `Options.Dither`
  施加抖动；位深向上或同深度转换**绝不**引入额外噪声。
- `DitherTPDF`：三角 PDF 抖动（两个 [0,1) LSB 均匀变量之差，峰峰值
  2 LSB），可完全去相关量化误差。
- 抖动噪声由 splitmix64 从 `DitherSeed` 生成，噪声序列只取决于种子与
  已产出的样本数：**同一种子结果必然可复现**，且与分块方式无关。

### 声道混音（确定性规则）

1. 声道数相同：恒等，逐样本透传，无串音。
2. N → 1（下混单声道）：各输入增益 `1/N`，输出为平均值。
3. 1 → M（单声道扩展）：样本以增益 1 复制到每个输出声道。
4. N → M（N > M > 1）：输入声道 j 以增益 `1/k` 送入输出声道
   `j mod M`（k 为映射到该输出的输入数），即轮转平均。
5. N → M（M > N > 1）：前 N 声道增益 1 直拷，**其余声道为静音**
   （精确的 0）。

## 错误语义

所有失败都返回 `*pcm.Error`，`Kind` 相互区分，`Offset` 为输入流中的
绝对字节位置（配置类错误为 -1）：

| Kind | 含义 |
|---|---|
| `ErrKindUnsupported` | 不支持的格式/配置组合 |
| `ErrKindCorrupt` | 数据无法解码（如 NaN/Inf 浮点样本） |
| `ErrKindOutOfRange` | 浮点样本越出 [-1, 1] |
| `ErrKindIncompleteFrame` | 输入在帧中间结束（`Close` 时发现） |

- `Write` 出错时返回已消费的字节数，**失败帧及其后数据被丢弃**，
  调用方跳过/修复坏字节后可继续 `Write`——单条流出错不会拖垮
  管线，更不会影响其他管线实例。
- 不完整帧不会被静默截断或补零：`Close` 返回
  `ErrKindIncompleteFrame` 且**不关闭**管线，补齐缺失字节后可再次
  `Close` 正常收尾。

## 测试

```
go test ./...
```

覆盖：分块无关性（含重采样+抖动的逐字节一致）、采样率互转（帧数
守恒、正弦频率保持、抗混叠）、位深与抖动（可复现性、向上/同深度
无噪声）、声道混音矩阵、各类异常输入与管线隔离。
