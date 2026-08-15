# cava-go

Windows 终端音频可视化器 —— Linux [cava](https://github.com/karlstav/cava) 的 Go 复刻：捕获系统正在播放的音频（WASAPI loopback），经 FFT 处理以彩色柱状图实时渲染到终端。

## 特性

- **捕获系统输出音频**（WASAPI loopback，事件驱动，无需声卡「立体声混音」）
- **柱状频谱**：对数频率刻度、半块字符 2 倍垂直精度、自动增益（autosens）
- **双色垂直渐变**（可配置）、峰值回落动画（falloff）、相邻柱条平滑
- **120fps 平滑渲染**（差异刷新，仅更新变化的格子）
- **TOML 配置 + 热重载**（`r` 键）
- 纯 Go、单二进制、无 cgo，仅支持 Windows（Windows 10 1809+ / Windows Terminal）

## 构建与运行

```bash
go build -o cava.exe ./cmd/cava
# 带版本号构建（-version 显示）
go build -ldflags "-X main.version=v1.0.0" -o cava.exe ./cmd/cava
```

或直接运行：`go run ./cmd/cava`。

首次运行会在 `%APPDATA%/cava-go/config.toml` 自动生成**带注释的默认配置**。

## 快捷键

| 键 | 功能 |
|---|---|
| `q` / `Esc` / `Ctrl-C` | 退出 |
| 空格 | 暂停 / 恢复（画面冻结） |
| `=` / `-` | 增大 / 减小灵敏度倍率（步进 0.2，实时生效） |
| `r` | 热重载配置 |

## 配置

配置文件默认位于 `%APPDATA%/cava-go/config.toml`，可用 `--config <path>` 指定其他路径。修改后按 `r` 热重载；**结构性参数**（`bars` / `fft_size` / `hop` / 频率范围）变更需重启生效。

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `[general] fps` | `120` | 渲染帧率（帧/秒） |
| `[general] bars` | `51` | 柱状条数量（显示时按终端宽度自适应减少） |
| `[general] autosens` | `true` | 自动增益：让最高柱条接近 `target_peak` 高度 |
| `[general] sensitivity` | `1.0` | 固定灵敏度倍率（autosens 关闭时生效） |
| `[dsp] fft_size` | `2048` | FFT 窗口大小（采样点数） |
| `[dsp] hop` | `512` | 相邻 FFT 分析间隔（越小分析越频繁，动画越平滑） |
| `[dsp] min_freq` | `20` | 最低分析频率（Hz） |
| `[dsp] max_freq` | `20000` | 最高分析频率（Hz） |
| `[dsp] target_peak` | `0.8` | autosens 目标峰值高度（0~1） |
| `[smoothing] falloff` | `3.0` | 柱条回落速度（每秒衰减比例，`0` 为关闭） |
| `[smoothing] smooth_bars` | `true` | 相邻柱条平滑 |
| `[color] gradient_bottom` | `#0000A0` | 渐变底部颜色（`#RRGGBB`） |
| `[color] gradient_top` | `#00FFFF` | 渐变顶部颜色（`#RRGGBB`） |
| `[keys] quit` | `q` | 退出键 |
| `[keys] pause` | `空格` | 暂停键 |
| `[keys] sens_up` | `=` | 增大灵敏度键 |
| `[keys] sens_down` | `-` | 减小灵敏度键 |
| `[keys] reload` | `r` | 热重载键 |

## 技术栈

- [go-wca](https://github.com/moutend/go-wca) —— WASAPI loopback 音频捕获
- [gonum dsp/fourier](https://pkg.go.dev/gonum.org/v1/gonum/dsp/fourier) —— FFT
- [tcell/v2](https://github.com/gdamore/tcell) —— 终端渲染
- [go-toml/v2](https://github.com/pelletier/go-toml) —— 配置解析

## 文档

- [docs/DESIGN.md](docs/DESIGN.md) —— 设计文档（架构、算法、变更记录）
- [docs/PROJECT.md](docs/PROJECT.md) —— 开发规范、任务拆分与进度

## 开发

```bash
go test ./...   # 全部单测
go vet ./...    # 静态检查
```
