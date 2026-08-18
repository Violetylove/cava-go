# AGENTS.md — cava-go

Windows 终端音频可视化器（Linux cava 的复刻，Go 实现）：捕获系统正在播放的音频（Windows WASAPI / **Linux PulseAudio**）→ FFT 频谱 → tcell 终端彩色柱状图动画。**已发布 v1.0.0（M0-M5 全部完成），Windows + Linux 双平台**。

## Project

- 纯 Go（**全平台无 cgo**，Go 1.26），module `cava-go`；支持 Windows（WASAPI）+ Linux（PulseAudio 纯 Go native protocol 客户端）
- 技术栈：`moutend/go-wca`（WASAPI 捕获）、`gonum dsp/fourier`（FFT）、`gdamore/tcell/v2`（终端渲染）、`pelletier/go-toml/v2`（配置解析）、`go-ole`、`x/sys/windows`
- 入口：`cmd/cava/main.go`（flag：`-config`、`-duration`、`-version`；运行时 q/Esc/Ctrl-C 退出）
- 设计事实来源 `docs/DESIGN.md`；执行/进度/开发规范事实来源 `docs/PROJECT.md`（§3 开发规范、§4 任务拆分、§5 进度跟踪）；用户使用说明 `README.md`

## Commands

- `go build ./...` / `go vet ./...` / `go test ./...` — 构建 / 静态检查 / 测试（提交前必须全绿）
- `go run ./cmd/cava` — 运行可视化器（需真实终端，建议 Windows Terminal）
- `go build -ldflags "-X main.version=vX.Y.Z" -o cava.exe ./cmd/cava` — 带版本号发布构建
- **交叉编译**（无 cgo）：`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o cava-linux ./cmd/cava`，Windows 侧任意平台可交叉编译，Linux 版无需 libpulse-dev
- 无 lint/CI 配置；验证靠 `gofmt` + `go vet` + `go test`

## Architecture

数据流：capture goroutine → dsp → render 主循环（`pipe.Latest()` 拉取最新帧，丢中间帧保实时）。

- `internal/audio` — `AudioSource` 接口（`Start() (<-chan []float32, error)` / `SampleRate()` / `Close()`）；`NewSource()` 工厂按平台选择：Windows `WasapiSource`（WASAPI loopback 事件驱动）、Linux `PulseSource`（**纯 Go PulseAudio native protocol**：`pulse_proto.go` 协议层 + `pulse_linux.go` 实现，`@DEFAULT_MONITOR@` 录制、SCM_CREDENTIALS 认证、10ms 数据切片投递）；共享 PCM 归一化混单声道与 `RMS()`
- `internal/dsp` — `Pipeline`：环形缓冲分帧 → Hann 窗（含增益补偿）→ gonum FFT → 对数频率映射（bin→bar）→ 峰值导向 autosens（attack/release 非对称平滑）→ **时间驱动** falloff（Latest 按真实时间衰减，音频流停止也回落）→ smooth-bars 卷积；并发安全（内部 mutex）；`SetSensitivity` 运行时热调
- `internal/vis` — 数据契约 `Frame`；`Cell{Rune, Fg, Bg}`（Fg/Bg = 上下半块的渐变级，双色渲染）；`RenderSpectrum`：平头半块字符（`▀▄█`）、静音死区（不足 1 半块归零）、柱宽自适应 + 间隔 + 溢出均匀减柱
- `internal/render` — tcell 渲染器：**差异刷新**（只更新变化格子并擦除消失的格子，resize 全量重绘）、可变 FPS（默认 120）、`SetGradient`/`SetFPS` 热更新、按键 action 上报、退出时 stderr 打印 fps/avg/peak max bar 统计
- `internal/config` — TOML 配置：加载/默认/校验/首次生成带注释模板（`[general]`/`[dsp]`/`[smoothing]`/`[color]`/`[keys]` 五节）
- `cmd/cava` — 装配三 goroutine + 配置驱动 + 快捷键（空格暂停、`=`/`-` 灵敏度、`r` 热重载）+ 中文可读启动报错（`fatal`）

## Conventions

- **Git**：主分支 `main`；任务分支 `feature/<task-id>-<slug>` → squash merge；提交信息**约定式提交、英文**（`feat(audio): ...`）；语义化版本标签（详见 PROJECT.md §3.1）
- **代码**：`gofmt`/`go vet` 零告警；错误用 `fmt.Errorf("...: %w", err)` 包装不吞错；**代码与注释英文**、文档中文；`internal/dsp` 核心算法必须有单测
- **架构约束（禁止违反）**：依赖单向 `cmd → internal/*`；DSP/渲染层**禁止出现 WASAPI 类型**（必须走 `audio.AudioSource` 接口）；bar 帧统一为 `[]float32`（0..1）
- **运行期零终端输出**：tcell 画面激活期间禁止 log/fmt 写 stderr（会污染画面且差异刷新不修复被覆盖区域），日志仅限启动错误（tcell 初始化前）与退出统计（画面还原后，见 PROJECT.md 附录 D）
- **文档同步**：实现完成后更新 `docs/PROJECT.md` §5 进度跟踪；设计变更更新 `docs/DESIGN.md` §7 变更记录；新增第三方依赖需先讨论
- **Windows 特有**：`go-wca` 的 `WAVEFORMATEX` Go 结构有尾部 padding（18→20 字节），**禁止用于内存映射**——mix format 必须按 `internal/audio/convert.go` 的固定字节偏移（`off*` 常量）解析；`SetEventHandle` 用 `x/sys/windows.CreateEvent`（EVENT_ALL_ACCESS）；`GetBuffer` 的 `AUDCLNT_S_BUFFER_EMPTY`（0x08890001）是成功码需按空包处理

## Notes

- **状态：v1.0.0 已发布**（M0-M5 全部完成）；平台：Windows + Linux（2026-08-16 新增）；**全平台无 cgo**（2026-08-17 Linux 后端去 cgo，任意平台可交叉编译）；已知限制：macOS 未实现、真彩色不降级、默认设备切换不自动重连、仅 spectrum 一种可视化、Linux 要求 PulseAudio 协议 ≥ 34（PA 14.0+，老版本不支持）
- **Linux 构建**：无外部依赖（纯 Go native protocol，无需 libpulse-dev）；`CGO_ENABLED=0 GOOS=linux` 任意平台交叉编译；CI：`test` job 双平台（windows/ubuntu）原生跑 vet/test，`release` job 用 **`wangyoucao577/go-release-action` 矩阵**（windows/amd64 + linux/amd64）交叉编译并上传 Release assets
- 已知坑与调试经验：见 `docs/PROJECT.md` 附录 D（go-wca padding、AUDCLNT_S_BUFFER_EMPTY、Hann 窗补偿、RMS vs 峰值增益、终端无真圆角、运行期 stderr 日志污染、时间驱动 falloff、instFloor 死区、CRLF 行尾噪音、**PulseAudio native protocol 要点：pstream 帧头/命令号/TLV 编码随协议版本变化、SCM_CREDENTIALS 认证、数据突发需 10ms 切片、WSLg PULSE_SERVER 路径陷阱**）
- go-wca v0.3.0 模块不含 `_example`（示例只在 GitHub 仓库）；离线开发以 `pkg/wca` 源码为准
- （待补充）
