# cava-go 项目说明书

> 本说明书用于**约束和记录** cava-go 项目的开发：目标、技术选型、开发规范、任务拆分、进度、里程碑与风险。
> 本文档为**项目执行与进度的事实来源**；设计细节（架构、算法）见 [docs/DESIGN.md](./DESIGN.md)，两者互相引用。

---

## 1. 项目概述

cava-go 是 Linux 终端音频可视化器 **cava** 的 **Windows 复刻**（Go 实现）：捕获系统正在播放的音频（WASAPI Loopback），经 FFT 处理后以频谱柱状图、波形等方式实时渲染到终端。

- **定位**：单二进制、低 CPU 占用、可配置、观感对标 cava。
- **目标平台**：Windows 10 1809+（Windows Terminal / ConHost），架构上预留 Linux 后端。
- **非目标**：音频播放/录制/导出、GUI、移动端。
- 详细任务目标与特性对照见 DESIGN.md §1、§2。

---

## 2. 技术选型

| 层 | 选型 | 说明 |
|---|---|---|
| 语言 | Go（≥1.22） | 单二进制、无 cgo、goroutine 流水线 |
| 音频捕获 | `github.com/moutend/go-wca` | WASAPI loopback + 事件驱动，官方示例为模板 |
| 信号处理 | `gonum.org/v1/gonum/dsp/fft` | 实数 FFT、窗函数 |
| 终端渲染 | `github.com/gdamore/tcell/v2` | 真彩色、脏矩形刷新、Windows 支持 |
| 配置解析 | `github.com/pelletier/go-toml/v2` | TOML |

**选型约束（强制）**：依赖仅限上表；新增第三方依赖须先在说明书中讨论并记录理由。备选方案及排除原因见 DESIGN.md §3.2。

**module 名**：暂定 `cava-go`（本地开发）；若发布到 GitHub，改为 `github.com/<owner>/cava-go`（全局替换，无行为影响）。

---

## 3. 开发规范

### 3.1 Git 规范

- **主分支**：`main`（唯一长期分支；`git init -b main`）。
- **分支命名**（任务分支从 `main` 拉出，按任务 ID 命名）：
  - `feature/<task-id>-<slug>`，如 `feature/M2-T2-frequency-map`
  - `fix/<slug>`、`docs/<slug>`、`chore/<slug>`
- **提交信息：遵循约定式提交（Conventional Commits），使用英文。**
  - 格式：`<type>(<scope>): <subject>`
  - `type`：`feat` / `fix` / `docs` / `refactor` / `test` / `perf` / `chore` / `build` / `ci` / `style`
  - `scope`（可选）：`audio` / `dsp` / `vis` / `render` / `config` / `cli` / `docs` / `build`
  - `subject`：祈使句、小写开头、≤72 字符、不加句号
  - 一条提交只做一件事；允许 body 补充动机（`why`）。
  - 示例：
    ```
    feat(audio): add wasapi loopback capture
    feat(dsp): implement frequency-to-bar mapping
    fix(render): handle terminal resize correctly
    refactor(dsp): extract windowing into separate package
    test(dsp): add falloff smoothing unit tests
    docs: update design doc for M2
    chore: bump go-wca dependency
    ```
- **合并策略**：任务分支完成自测后 **squash merge** 到 `main`，`main` 保持线性整洁；merge 提交信息沿用任务分支的首个约定式提交。
- **版本标签**：语义化版本 `vX.Y.Z`，打在 `main` 上（见 §6）。

### 3.2 Go 代码规范

- `gofmt` 必须通过；`go vet ./...` 必须零告警。
- 导出符号必须有英文注释；包名小写、单数。
- **错误处理**：不吞错；用 `fmt.Errorf("...: %w", err)` 包装并携带上下文；可恢复错误要记录，不可恢复的要向上传播到 `cmd/cava` 统一处理。
- **代码与注释使用英文**；文档（md）使用中文。
- **测试**：`internal/dsp` 的核心算法（加窗/映射/平滑/增益）必须有单元测试；`internal/audio` 通过接口 mock 测试上层。
- 日志：初期使用标准库 `log`，输出到 stderr。

### 3.3 架构约束

- 依赖方向单向：`cmd/cava` → `internal/*`；`internal/audio`、`internal/dsp`、`internal/vis`、`internal/render`、`internal/config` 之间禁止反向依赖。
- `AudioSource` 接口隔离（DESIGN.md §3.3）：**DSP/渲染层禁止出现 WASAPI 类型**，未来换 Linux 后端零改动。
- 帧数据契约 `VisFrame`（DESIGN.md §5.5）为 DSP 与渲染层唯一接口，不得绕过。

### 3.4 文档规范

- `docs/DESIGN.md`：设计事实来源。**每次实现完成后**，更新 §8 状态跟踪（日期/里程碑/内容/验证/遗留）；设计变更更新 §9 变更记录。
- 本文档：任务完成后勾选 §5 进度跟踪。

### 3.5 任务工作流

```
认领任务 → 从 main 拉分支(feature/<task-id>-<slug>)
  → 实现 → 自测(gofmt/vet/test/手动验证验收标准)
  → 约定式提交(英文) → squash merge 到 main → 更新进度跟踪
```

---

## 4. 任务拆分（WBS）

按里程碑拆分为任务（T）；每个任务给出验收标准，必要时细分步骤。里程碑总览见 §4.1，任务清单见 §4.2。

### 4.1 里程碑总览

| 里程碑 | 内容 | 完成标准 |
|---|---|---|
| **M0** 项目初始化 | 仓库（main 分支）、依赖、目录骨架、文档落位 | `go build` / `go vet` 通过，文档入库 |
| **M1** 音频链路 | go-wca loopback 捕获 + 控制台打印 RMS 能量 | 播放音乐时能量波动、暂停归零 |
| **M2** 频谱渲染 | DSP 管线 + tcell + `spectrum` 柱状图 | 频谱随音乐变化、帧率 ≥30fps、CPU 占用合理 |
| **M3** 观感增强 | waveform 类型 + 颜色渐变 + falloff/平滑 + autosens | 视觉效果对标 cava，参数可调 |
| **M4** 配置与交互 | TOML 配置 + 快捷键 + resize + 真彩色降级 | 配置热重载生效，各终端行为正确 |
| **M5** 打磨发布 | 性能优化、错误处理与可读报错、打包发布、文档收尾 | 单二进制可直接运行，README/DESIGN 同步 |

### 4.2 任务清单

### M0 项目初始化

| ID | 任务 | 验收标准 |
|---|---|---|
| M0-T1 | 初始化仓库：`git init -b main`、`.gitignore`（Go）、`LICENSE` | main 分支存在；`git status` 干净 |
| M0-T2 | 初始化 `go.mod`（module `cava-go`），引入 §2 四个依赖 | `go build ./...` 通过 |
| M0-T3 | 建立目录骨架 `cmd/cava/` 与 `internal/{audio,dsp,vis,render,config}/`（各含占位包与注释） | 骨架可编译；`go vet` 零告警 |
| M0-T4 | 文档落位：DESIGN.md、PROJECT.md（本文档）入库 | 两文档齐全、相互引用正确 |

### M1 音频链路（验证 WASAPI Loopback）

| ID | 任务 | 验收标准 |
|---|---|---|
| M1-T1 | `internal/audio`：定义 `AudioSource` 接口；实现 wasapi 后端（go-wca 事件驱动 loopback） | 播放音乐时有 PCM 数据流出；暂停时数据仍在但能量为 0 |
| M1-T2 | PCM 归一化：兼容 int16/int32/float32 → `[]float32`；多声道混单声道 | 各位深输入输出均在 [-1,1]；混音无溢出 |
| M1-T3 | 能量检测：逐帧 RMS；低于阈值标记静音 | 静音判定与播放状态一致（见附录验证项） |
| M1-T4 | 临时验证命令（`cmd/cava` 雏形）：打印 RMS 能量到终端 | 播放音乐能量波动、暂停归零——**M1 完成标准** |

**M1 细分**：M1-T1 内部再分：① 枚举默认渲染端点 → ② 共享模式激活 + LOOPBACK 标志 → ③ 事件句柄 + 读取循环 → ④ 优雅关闭（退出时释放 COM/流）。

### M2 频谱渲染（首个可视化）

| ID | 任务 | 验收标准 |
|---|---|---|
| M2-T1 | `internal/dsp`：环形缓冲分帧 + Hann 加窗 + gonum 实数 FFT | 单测：正弦波输入在对应 bin 出现峰值 |
| M2-T2 | 频率映射：对数刻度 [min_freq, max_freq] → B 条柱（平均/峰值） | 单测：已知频谱映射结果符合预期 |
| M2-T3 | `internal/vis`：`VisFrame` 契约 + spectrum 绘制（半块字符 `▀▄█`） | 柱状图高度/宽度与 bar 数据一致 |
| M2-T4 | `internal/render`：tcell 渲染器、fps ticker（默认 30）、脏矩形刷新 | 运行中帧率稳定 ≥30fps |
| M2-T5 | 主程序装配（`cmd/cava`）：capture → dsp → render 三 goroutine + channel | 播放音乐看到频谱随节奏变化——**M2 完成标准** |

### M3 观感增强

| ID | 任务 | 验收标准 |
|---|---|---|
| M3-T1 | falloff 时间平滑（指数衰减、强度可配） | 单测：峰值回落符合衰减曲线 |
| M3-T2 | smooth-bars 空间平滑（相邻加权） | 单测：锯齿被削弱、总能量基本守恒 |
| M3-T3 | autosens 自动增益（滑动窗口 RMS 自适应 + 灵敏度覆盖） | 大小音量下频谱不过顶/不过矮 |
| M3-T4 | 颜色渐变：色带表 + 真彩色插值 + 256 色降级 | 渐变随高度变化；旧终端不花屏 |
| M3-T5 | waveform 可视化类型（时域波形） | 波形与播放内容同步、可切换 |

### M4 配置与交互

| ID | 任务 | 验收标准 |
|---|---|---|
| M4-T1 | TOML 配置：加载/校验/默认生成（对齐 cava 配置节，DESIGN.md §6.4） | 错误配置给出可读报错；`--config` 生效 |
| M4-T2 | 快捷键：`q`/空格/`Tab`/`+`/`-`/`r`（重载配置） | 各按键行为正确且不阻塞渲染 |
| M4-T3 | 终端适配：备用屏幕缓冲、隐藏光标、resize 重算布局 | 退出后终端画面完整还原；窗口拉伸不花屏 |
| M4-T4 | 配置驱动：bars、fps、可视化类型等运行时生效 | 改配置重载后立即反映 |

### M5 打磨发布

| ID | 任务 | 验收标准 |
|---|---|---|
| M5-T1 | 错误处理与可读报错（捕获失败/设备占用/终端不支持等） | 各失败场景给出中文可读提示而非 panic |
| M5-T2 | 性能优化：`go test -bench` + pprof 定位热点，控制 CPU | 默认配置 CPU 占用合理（目标 <10% 单核） |
| M5-T3 | 发布：交叉编译单二进制、版本标签、发布说明 | 免安装直接运行 |
| M5-T4 | 文档收尾：README 使用说明、DESIGN/PROJECT 同步 | 文档与实现一致 |

---

## 5. 进度跟踪

**里程碑进度**

| 里程碑 | 状态 | 完成日期 | 备注 |
|---|---|---|---|
| M0 项目初始化 | 完成 | 2026-08-05 | |
| M1 音频链路 | 完成 | 2026-08-05 | 实测：播放时 RMS 波动（峰值≈0.23）、静音归零；mix format = 48kHz float32 extensible |
| M2 频谱渲染 | 待办 | | |
| M3 观感增强 | 待办 | | |
| M4 配置与交互 | 待办 | | |
| M5 打磨发布 | 待办 | | |

基线注记：2026-08-05 设计文档初稿（自 DESIGN.md §8 迁入的历史记录）。

**任务进度**

| 任务 ID | 内容 | 状态 | 完成日期 | 备注 |
|---|---|---|---|---|
| M0-T1 | 初始化仓库（main 分支） | 完成 | 2026-08-05 | git init -b main |
| M0-T2 | go.mod 与依赖 | 完成 | 2026-08-05 | go-wca v0.3.0 / tcell v2.13.10 / gonum v0.17.0 / go-toml v2.4.3 |
| M0-T3 | 目录骨架 | 完成 | 2026-08-05 | go build/vet 通过 |
| M0-T4 | 文档落位 | 完成 | 2026-08-05 | DESIGN/PROJECT 已入库（b82107e） |
| M1-T1 | AudioSource + wasapi loopback | 完成 | 2026-08-05 | 事件驱动；go-wca 两个坑见附录 D |
| M1-T2 | PCM 归一化/混音 | 完成 | 2026-08-05 | int16/int32/float32 单测覆盖 |
| M1-T3 | 能量检测 | 完成 | 2026-08-05 | RMS 函数 + 单测 |
| M1-T4 | RMS 验证命令 | 完成 | 2026-08-05 | `cava -duration 14s` 实测通过 |
| M2-T1 | 分帧/加窗/FFT | 待办 | | |
| M2-T2 | 频率映射 | 待办 | | |
| M2-T3 | spectrum 绘制 | 待办 | | |
| M2-T4 | tcell 渲染器 | 待办 | | |
| M2-T5 | 主程序装配 | 待办 | | |
| M3-T1 | falloff 平滑 | 待办 | | |
| M3-T2 | smooth-bars | 待办 | | |
| M3-T3 | autosens | 待办 | | |
| M3-T4 | 颜色渐变 | 待办 | | |
| M3-T5 | waveform | 待办 | | |
| M4-T1 | TOML 配置 | 待办 | | |
| M4-T2 | 快捷键 | 待办 | | |
| M4-T3 | 终端适配 | 待办 | | |
| M4-T4 | 配置驱动 | 待办 | | |
| M5-T1 | 错误处理 | 待办 | | |
| M5-T2 | 性能优化 | 待办 | | |
| M5-T3 | 打包发布 | 待办 | | |
| M5-T4 | 文档收尾 | 待办 | | |

状态取值：`待办` / `进行中` / `完成`。

---

## 6. 版本计划

| 版本 | 包含 | 定义 |
|---|---|---|
| `v0.1.0` | M0 + M1 + M2 | 首个可用的频谱可视化 |
| `v0.2.0` | M3 | 观感增强（平滑/增益/渐变/waveform） |
| `v0.3.0` | M4 | 配置与交互完备 |
| `v1.0.0` | M5 | 打磨发布，功能对标 cava 核心集 |

---

## 7. 附录

### 附录 A：开发环境

- Windows / amd64；Go ≥1.22；`go build` 直接产出 exe。
- 验证手段：播放音乐（如浏览器/播放器）观察可视化；暂停验证归零；Windows Terminal 为推荐目标终端。

### 附录 B：待验证事项（进入 M1 前）

1. ~~go-wca `LoopbackCaptureSharedEventDriven` 示例在目标机器上的实际可用性~~ ✅ 2026-08-05 已验证（事件驱动 loopback 可用）；
2. ~~mix format 采样率/位深的实际分布~~ ✅ 本机实测 48kHz / 32-bit float / WAVEFORMATEXTENSIBLE / 2 声道；
3. tcell 在 Windows Terminal 与 ConHost 下的真彩色与刷新性能实测（M2 验证）；
4. 空闲（无音频播放）时 loopback 流的时序与能量行为，确认静音检测阈值（M1 已验证归零，阈值在 M3 autosens 时定）。

### 附录 C：风险与难点清单

| 风险 | 影响 | 缓解 |
|---|---|---|
| loopback 流无数据时行为（静音填充） | 画面噪声 | 能量检测（DESIGN.md §4.5） |
| 采样率/位深格式多样 | 兼容性问题 | 统一归一化 float32 + 采用 mix format（DESIGN.md §4.3） |
| 大终端逐帧刷新性能 | 掉帧/闪烁 | 脏矩形 + fps 限制（DESIGN.md §6.2） |
| 真彩色终端兼容 | 颜色失真 | 能力检测降级（DESIGN.md §6.3） |
| 默认设备切换 | 捕获中断 | 监听设备变化并自动重连（DESIGN.md §4.5） |
| go-wca 维护风险（社区较小） | 依赖风险 | 捕获层隔离在 `internal/audio`，可替换实现 |

### 附录 D：M1 关键经验（2026-08-05）

1. **go-wca 的 `WAVEFORMATEX` 不能做内存映射**：Go 把结构体大小向上取整到对齐倍数（18→20 字节），导致后续字段全部偏移。mix format 必须按**固定字节偏移**解析（见 `internal/audio/convert.go` 的 `off*` 常量）。
2. **`SetEventHandle` 报拒绝访问**：事件句柄需 `EVENT_ALL_ACCESS` 权限；直接用 `golang.org/x/sys/windows.CreateEvent`（内部用 EVENT_ALL_ACCESS），不要用 `wca.CreateEventExA(..., 0)`。
3. **`GetBuffer` 返回 `AUDCLNT_S_BUFFER_EMPTY`（0x08890001）**：这是**成功码**（bit31=0），但 go-wca 把所有非 0 HRESULT 当错误返回。需用 `ole.OleError.Code()` 识别并视为“无数据，等下一事件”，否则捕获立即退出。
4. **事件驱动 + loopback 实测工作正常**：静音时输出全 0 帧（loopback 静音填充），播放时能量波动，时序稳定。
