# cava-go 设计文档

> cava（**C**onsole-based **A**udio **V**isualizer for **A**lsa）的 Windows 复刻。
> 本文档是项目的**设计事实来源**；任务拆分、进度跟踪、里程碑与开发规范见 [docs/PROJECT.md](./PROJECT.md)。变更记录见 §7。

---

## 1. 背景与目标

### 1.1 cava 是什么

cava 是 Linux 下经典的终端音频可视化器（C 语言编写）：

- 通过音频后端（ALSA / PulseAudio / PipeWire / sndio / JACK）捕获**系统正在播放的音频**（而非麦克风输入）；
- 对 PCM 数据做 FFT 等处理，在终端里渲染出频谱柱状图（spectrum）、波形（waveform）、频谱图（sgram）、示波器（wavescope）等可视化效果；
- 通过 ncurses 逐帧刷新终端，支持颜色渐变、自动增益、平滑、可配置参数（`~/.config/cava/config`）。

### 1.2 本项目目标

在 **Windows 平台**上实现功能对等的 cava 复刻，做到：

1. **捕获系统音频输出**（"听到什么画什么"）——Windows 上的对应物是 **WASAPI Loopback**；
2. 提供与 cava 对齐的**可视化类型**与**观感**（频谱柱状图是第一优先级）；
3. 低 CPU 占用、可配置、单二进制发布。

### 1.3 非目标（明确不做）

- 不播放、不录制、不导出音频；
- 不做音频设备管理 GUI；
- 首期只面向 Windows（Windows 10 1809+ / Windows Terminal），但**架构上预留 Linux 后端**（ALSA/PulseAudio），便于未来跨平台。

### 1.4 语言选型

仓库名为 `cava-go`，默认采用 **Go**：

- 单二进制、交叉编译方便（`GOOS=windows` 即可发布）；
- goroutine + channel 天然契合"捕获 → 处理 → 渲染"的流水线；
- 关键库均为纯 Go（见 §3.2），无需 cgo，无需 C 工具链，Windows 上开发体验好。

---

## 2. cava 特性对照与调研结论

### 2.1 特性对照表

| cava 特性 | Windows 对应实现 | 优先级 |
|---|---|---|
| ALSA/Pulse/PipeWire 捕获 | WASAPI Loopback（共享模式） | P0 |
| spectrum（频谱柱状图） | 柱状图 + 半块字符 | P0 |
| waveform（时域波形） | 波形绘制 | 取消（2026-08-06，只做柱状图） |
| spectrum_waveform（叠加） | 上频谱下波形 | P2 |
| wavescope（示波器） | 扫线示波器 | P2 |
| sgram（频谱图） | 频谱随时间滚动 | P2 |
| 自动增益 autosens | 滑动窗口 RMS 动态增益 | P1 |
| 平滑（falloff / smooth-bars） | 指数衰减 + 相邻 bar 加权 | P1 |
| 颜色渐变（可配置色带） | 真彩色渐变（降级 256 色） | P1 |
| 帧率控制 | render ticker（默认 30/60fps） | P1 |
| 配置文件 `~/.config/cava/config` | TOML（对齐 cava 配置节） | P1 |
| 键盘交互 | tcell 事件 + 快捷键 | P2 |
| 立体声/多声道 | 混单声道（可配左右分离） | P2 |

### 2.2 关键调研结论（2026-08 验证）

| 环节 | 选型 | 证据/理由 |
|---|---|---|
| 音频捕获 | `github.com/moutend/go-wca` | README 明确支持 **Loopback capturing with shared event mode / timer mode**，仓库自带 `LoopbackCaptureSharedEventDriven` 官方示例；纯 syscall 无 cgo，覆盖 IAudioClient/IAudioCaptureClient 全套 COM 接口 |
| 终端渲染 | `github.com/gdamore/tcell/v2` | 官方支持 **24-bit 真彩色**、Windows 平台、**脏单元跟踪**（逐帧只发送变化的 cell，适合频谱刷新）；活跃维护 |
| FFT | `gonum.org/v1/gonum/dsp/fourier` | 实/复 FFT、DCT 及窗函数（`dsp/window` 子包）；生产级、持续维护 |
| 配置解析 | `github.com/pelletier/go-toml/v2` | Go 生态最成熟的 TOML 库 |

---

## 3. 总体架构

### 3.1 分层与数据流

```
┌────────────┐   PCM帧   ┌────────────┐   bar数据  ┌────────────┐
│  Audio     │ ────────▶ │  DSP       │ ────────▶ │  Renderer  │
│  Capture   │  channel  │  Pipeline  │  channel  │  (tcell)   │
│  (go-wca)  │           │  (gonum)   │           │            │
└────────────┘           └────────────┘           └────────────┘
       ▲                       ▲                        ▲
       └────────── Config (TOML，横切) ──────────────────┘
```

三个 goroutine 组成流水线，通过 channel 传递数据：

1. **capture goroutine**：WASAPI 事件驱动，音频事件到达即读取 PCM 数据，归一化为 `[]float32`（单声道混音）后投递；
2. **processor goroutine**：分帧 → 加窗 → FFT → 频率映射 → 平滑 → 增益，产出每帧的 bar 高度 `[]float32`（范围 0~1）与可选的波形采样点；
3. **render goroutine**：按目标帧率 tick，消费最新一帧可视化数据，构建字符帧缓冲，经 tcell 刷新到终端。

**背压与丢帧策略**：`channel` 带缓冲（如 2~4 帧）；渲染端消费慢时，processor 产出**覆盖旧帧**（最新值优先），宁可丢中间帧也不让实时性被拖垮——可视化追求实时，不追求不丢帧。

### 3.2 技术选型与备选方案

**选定组合（全链路纯 Go、无 cgo、无外部二进制）：**

| 层 | 库 | 备注 |
|---|---|---|
| 捕获 | `moutend/go-wca` | loopback + 事件驱动，官方示例直接当模板 |
| 处理 | `gonum.org/v1/gonum/dsp/fourier` | 实数 FFT（`dsp/window` 提供窗函数） |
| 渲染 | `gdamore/tcell/v2` | 真彩色、脏矩形刷新、Windows 支持 |
| 配置 | `pelletier/go-toml/v2` | TOML 解析 |

**曾考虑的备选（及不选原因）：**

- **CGo + Windows SDK 直调 WASAPI**：可行但引入 cgo 与 COM 样板代码，收益低于直接用 go-wca；
- **ffmpeg dshow**（`-f dshow -i audio=virtual-audio-capturer`）：依赖外部 ffmpeg 二进制，且依赖声卡"立体声混音"驱动，不符合单二进制目标；
- **NAudio/.NET 互操作**：功能全但把 .NET 运行时拖进纯 Go 项目，不推荐；
- **PortAudio（gordonklaus/portaudio）**：Go 绑定未暴露 WASAPI loopback 标志，不可行；
- **termbox-go**：Windows 后端走 Win32 console API、无真彩色、长期停滞，不如 tcell。

### 3.3 包结构建议

```
cava-go/
├── cmd/cava/            main：装配、信号/错误处理
├── internal/audio/      AudioSource 接口 + wasapi 实现（loopback 捕获）
├── internal/dsp/        加窗 / FFT 封装 / 频率映射 / 平滑 / 增益
├── internal/vis/        可视化类型（spectrum / waveform / ...）与帧缓冲
├── internal/render/     tcell 渲染器、颜色渐变、终端适配
├── internal/config/     配置加载与校验（TOML）
└── docs/DESIGN.md       本文档
```

`internal/audio` 抽象出 `AudioSource` 接口（`Start() <-chan []float32`），未来接入 ALSA/PulseAudio 后端时（通过 cgo 或外部进程）只需新增实现，DSP 与渲染层零改动。

---

## 4. 音频捕获子系统（核心难点）

### 4.1 WASAPI Loopback 原理

Windows 上捕获"系统正在播放的音频"的标准途径是 **WASAPI loopback**：

- 打开默认渲染端点（`IDeviceEnumerator` → 默认 output device）；
- 以 **共享模式** 激活 `IAudioClient`，stream flags 带 `AUDCLNT_STREAMFLAGS_LOOPBACK`；
- loopback 流以**静音填充**代替输出流（静默期依然有数据，能量为 0），因此必须靠能量检测区分"真静音"与"无数据"——见 §4.5。

### 4.2 事件驱动 vs 定时器驱动

- **定时器驱动（timer-driven）**：按固定间隔轮询 `GetCurrentPadding`，实现简单但有空转与延迟抖动；
- **事件驱动（event-driven）**：`SetEventHandle` + `AUDCLNT_STREAMFLAGS_EVENTCALLBACK`，音频引擎就绪时发事件，配合 goroutine 阻塞等待事件，CPU 占用低、延迟稳定。

**选型：事件驱动**（go-wca 的 `LoopbackCaptureSharedEventDriven` 示例为模板）。

### 4.3 采样率 / 位深 / 声道

- **采样率**：直接采用设备共享模式的 **mix format**（通常 48kHz），首期**不做重采样**，避免引入 SRC 复杂度；若未来要支持任意采样率，再加采样率转换层；
- **位深**：兼容 `WAVEFORMATEX` / `WAVEFORMATEXTENSIBLE`，把 int16/int32/float32 统一归一化为 **float32**（-1.0~1.0）进入管线；
- **声道**：默认多声道混音为单声道（简单平均）；配置可切"左右分离"模式（两路独立 pipeline 或仅渲染左侧）。

### 4.4 备选捕获途径脑暴（均已排除，理由见 §3.2）

| 途径 | 结论 |
|---|---|
| `winmm` waveIn | 只能录**输入**设备（麦克风），无法捕获系统输出 ✗ |
| 立体声混音（Stereo Mix）驱动 | 依赖声卡驱动支持、Windows 10 起逐渐消失 ✗ |
| ffmpeg dshow `virtual-audio-capturer` | 外部二进制依赖 ✗ |
| PortAudio WASAPI | Go 绑定未暴露 loopback ✗ |
| NAudio/.NET | 引入 .NET 运行时 ✗ |

### 4.5 静音与故障处理

- **静音检测**：帧 RMS 低于阈值视为静音，可跳过 FFT（省 CPU），渲染端显示空频谱；
- **设备变化**：默认设备切换/采样率变化时重启捕获流（先监听 `IAudioEndpointVolume` 或定期校验）；
- **无播放音频**：loopback 流依然产生静音数据，能量检测保证画面归零而非闪烁噪声；
- **独占模式冲突**：若其他程序占用了独占模式，共享模式 loopback 会失败——捕获错误并给出可读提示。

---

## 5. 音频处理管线

处理顺序（processor goroutine 内逐帧执行）：

```
PCM帧(float32[]) → 分帧 → 加窗(Hann) → 实数FFT → 幅度谱
  → 频率映射(对数刻度, bar 取区间平均/峰值) → 时间平滑(falloff)
  → 空间平滑(smooth-bars) → 自动增益(autosens) → bar数据(0~1)
```

### 5.1 分帧与加窗

- 帧大小 N（默认 2048，可配 1024/4096），与 FFT 大小一致；
- 帧率与 hop：48kHz、60fps 下每帧约 800 samples，用**环形缓冲**累积到 N 后再 FFT（相邻帧有重叠，相当于 50% overlap 的变体），避免 hop=N 导致的时间分辨率不足；
- 加窗：默认 **Hann**（旁瓣抑制好），可配 Blackman / Hamming / 不加窗。

### 5.2 频率映射（bin → bar）

- 条柱数 B（默认 30~60，对应终端宽度的一半左右，可配）；
- 采用**对数/感知刻度**（与 cava 一致）：配置 `min_freq`（默认 20Hz）、`max_freq`（默认 20kHz），把 [min, max] 在对数轴上分成 B 段，每段覆盖若干 FFT bin，取**平均或峰值**作为 bar 高度；
- 线性刻度作为备选配置项。

### 5.3 平滑

- **时间平滑（falloff）**：bar 下降时按指数衰减（`falloff` 系数可配），上升跟随瞬时值，产生"峰值回落"的经典观感；
- **空间平滑（smooth-bars）**：相邻 bar 加权平均（如 `[0.25, 0.5, 0.25]` 卷积），消除锯齿；
- 两者都开关可配、强度可配。

### 5.4 自动增益（autosens）

- **峰值导向**（2026-08-06 修订，替代初稿的 RMS 方案）：对瞬时 bar 帧的最大值做低通平滑（`smoothPeak`，α=0.2），增益 = 目标峰值（默认 0.8）/ smoothPeak，钳制 [0.2, 15]；
- 因增益作用于峰值 bar 本身，数学上保证最高 bar 持续接近目标高度（bars ≈ target），不受输入音量影响——M3 实测播放时段 max bar ≈ 0.8（初稿的 RMS 方案受 crest factor 限制、提升有限，已废弃）；
- **响应速度**：增益用非对称 attack/release——上升快（α=0.3，≈64ms 收敛）以跟随瞬时动态，下降慢（α=0.05）避免对瞬态峰值“泵动”；此前对称 α=0.05 造成 ~0.6s 滞后（用户实测感知 ~1s 延迟）；
- 用户可设固定 `sensitivity` 覆盖自动模式（`AutoSens=false` 时生效）。

### 5.5 输出契约

- 每帧输出 `VisFrame{ Bars []float32 /*0~1*/, Wave []float32 /*时域可选*/, ... }`；
- 可视化类型（§6.1）只消费 `VisFrame` 的相应字段，DSP 层与渲染层解耦。

---

## 6. 可视化、配置与交互

### 6.1 可视化类型（对齐 cava）

| 类型 | 说明 | 优先级 |
|---|---|---|
| `spectrum` | 频谱柱状图，**唯一实现的可视化** | P0 |
| `waveform` | ~~时域波形~~ 已取消（2026-08-06 用户决策：只做柱状图） | — |
| `spectrum_waveform` | 上半频谱 + 下半波形叠加（依赖 waveform，一并取消） | — |
| `wavescope` | 示波器扫线（能量调制宽度） | P2 暂不实现 |
| `sgram` | 频谱图：频谱随时间向上/下滚动 | P2 暂不实现 |

### 6.2 渲染管线

- **字符帧缓冲**：二维 cell 数组，`cell = rune + Style(前景/背景真彩色)`；
- **半块字符提升垂直分辨率**：用 `▀`(上块)/`▄`(下块)/`█`(全块)/空格 组合，使每行字符可表达 2 像素高度——这是终端频谱视觉质量的关键；柱顶保持**平头**（cava 风格）：字符网格最小单元是直角四分之一块，无法表达真圆弧；圆角由终端渲染层决定（如 kitty 的 `block_shape=rounded`，Windows Terminal 不支持）；
- **颜色渐变**：每根柱条**内部纵向**双色渐变（底部深蓝 → 顶部亮青）；每字符格用**前景+背景双色**渲染（前景=填充半块色、背景=另一半块色），垂直色阶翻倍（30 行 → 60 色阶），渐变平滑且半块柱顶不再露出黑色背景缝；优先真彩色（Windows Terminal），检测不支持时降级 256 色/16 色；
- **脏矩形刷新**：tcell 只发送变化的 cell，大幅降低输出字节量，避免大终端下闪烁/滚动条抖动；
- **帧率控制**：render ticker 固定 fps（默认 60，实测精确 60.0fps 不掉帧），防过度刷新。

### 6.3 终端适配（Windows 特有）

- 进入/退出**备用屏幕缓冲**（alternate buffer），退出时还原用户终端画面；
- **隐藏光标**，退出恢复；
- **尺寸变化检测**：Windows 无 SIGWINCH，监听 tcell 的 `EventResize`（底层轮询 console 尺寸），重算条柱数与布局；
- **VT 能力检测**：Windows 10 1809+ ConHost / Windows Terminal 均支持；极旧 ConHost 报错或降级提示。

### 6.4 配置系统（TOML，对齐 cava 配置节）

```
[general]    fps, bars, autosens, sensitivity
[input]      method=wasapi, sample_rate, channels, mono
[dsp]        fft_size, window, min_freq, max_freq, bar_scale
[smoothing]  falloff, smooth_bars
[color]      gradient(色带定义), use_truecolor
[visual]     type=spectrum, bar_width, bar_spacing
[keys]       快捷键绑定
```

- 支持 `--config <path>`、默认路径（如 `%APPDATA%/cava-go/config.toml`）、首次运行生成默认配置。

### 6.5 交互（P2）

- 快捷键：`q` 退出、空格 暂停/恢复、`Tab`/数字 切换可视化类型、`+`/`-` 调整灵敏度、`r` 重载配置；
- 事件处理非阻塞：渲染帧循环中 select 键盘事件，不阻塞画面。

---

## 7. 变更记录

| 日期 | 变更 | 原因 |
|---|---|---|
| 2026-08-05 | 创建本文档（初稿） | 启动 cava-go 项目，确定 Windows 复刻 cava 的总体设计 |
| 2026-08-05 | 里程碑计划、实现状态跟踪、风险清单、待验证事项迁出至 docs/PROJECT.md | 区分设计文档与项目管理文档：DESIGN.md 只保留设计内容，进度与规范归 PROJECT.md |
| 2026-08-05 | 修正 gonum FFT 库路径：`dsp/fft` → `dsp/fourier` | M2 实现时发现 gonum 实际无 `dsp/fft` 包（调研结论有误），FFT 在 `dsp/fourier`，窗函数在 `dsp/window` |
| 2026-08-06 | autosens 由 RMS 导向改为**峰值导向**（§5.4） | M3 实机反馈柱条偏低；RMS 方案受 crest factor 限制提升有限，峰值导向数学上保证最高 bar ≈ 目标高度 |
| 2026-08-06 | 取消 waveform 系列可视化（§2.1/§6.1），柱条数量 64→51、加宽（无间隔均分宽度） | 用户决策：只做柱状图并优化美观（数量-20%、宽度×2） |
| 2026-08-06 | 渐变改为**单柱纵向双色**（深蓝→亮青）；柱间恢复 1 列间隔，溢出时均匀减柱（§6.2） | 用户反馈：不要横向多色渐变；间隔后溢出可减少柱数 |
| 2026-08-06 | autosens 增加 attack/release 响应（§5.4）；falloff 默认 2→3/s；柱宽公式改为配额+1（上限 8） | 用户反馈：目测延迟 ~1s（根因：对称 α=0.05 平滑滞后）；柱条仍偏细 |
| 2026-08-06 | 渲染帧率 30→60fps；柱顶加 1px 圆角（四分之一块字符，§6.2） | 用户反馈：动画帧率太低；柱顶圆角更美观 |
| 2026-08-06 | 柱顶圆角**回退为平头方柱**（§6.2） | 用户反馈圆角效果为向内锯齿；经排查字符网格无法表达真圆弧（最小单元为直角四分之一块），圆角实为终端渲染层特性（如 kitty `block_shape=rounded`），Windows Terminal 不支持；平头在支持圆角化的终端下自动获得圆角 |
| 2026-08-06 | 渐变渲染改为**前景/背景双色半块**（§6.2）：移除半块柱顶的黑色背景缝，垂直色阶翻倍（60 色阶） | 用户反馈：柱顶有黑线、渐变呈小格子拼接（banding）；双色渲染同时解决两者 |
| 2026-08-15 | 静音**死区**：不足 1 半块高度的值归零（§6.2）；背景改终端默认色（不再强制纯黑） | 用户反馈：无音频时每根柱位出现 1px 蓝色细条（静音噪声×autosens 增益进位）；要求空白区域显示终端主题色 |

