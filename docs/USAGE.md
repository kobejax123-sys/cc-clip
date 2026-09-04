# cc-clip 使用文档

> cc-clip：把本地（Windows/macOS）剪贴板桥接到远程 Linux 主机，让远程终端里的 Claude Code / Codex 也能「粘贴图片」——本质是**剪贴板图片 → 上传成远程文件路径 → 粘贴路径**。

---

## 1. 一句话原理

常驻热键进程监听剪贴板，识别到图片复制时经 **SSH 长连接**把图片上传到远端生成路径，再把路径写回剪贴板、用合成 `Ctrl+Shift+V` 注入当前终端。

```
本地剪贴板图片 → 解码/编码 → SSH 连接池上传 → 远端 ~/.cache/cc-clip/uploads/clip-*.png
                                                              ↓
当前终端 ← Ctrl+Shift+V ← 剪贴板写入远程路径 ←──────────────┘
        （随后后台把原图片写回剪贴板，除非 --no-restore）
```

---

## 2. 初次安装（fork 版）

> **远端无需安装**：热键上传走 SSH 连接池内嵌 helper，自动下发到目标主机。
> **前置条件**：已配置到目标主机的免密 SSH（`~/.ssh/config` 别名）。

### 方式一：直接下载 exe（推荐，免编译）

最新版 **v0.14.6** 的 Release 已附带可直接运行的 `cc-clip.exe`：

1. 打开 `github.com/kobejax123-sys/cc-clip/releases/tag/v0.14.6`
2. 在 Assets 区下载 `cc-clip.exe`
3. 放到 PATH 目录下（Windows 示例：`C:\Users\<你的用户名>\.local\bin\cc-clip.exe`；macOS/Linux：`~/.local/bin/cc-clip`）
4. 验证：`cc-clip version` 应输出 `cc-clip v0.14.6`

> `~/.local/bin` 需已加入 PATH；若没有，把 `export PATH="$HOME/.local/bin:$PATH"` 追加到 shell 配置（`~/.bashrc` / `~/.profile`）。

### 方式二：源码构建（需要 Go 1.25+）

```bash
git clone https://github.com/kobejax123-sys/cc-clip.git
cd cc-clip
git checkout feature/clipboard-copy-image
go build -o ~/.local/bin/cc-clip.exe ./cmd/cc-clip
cc-clip version   # 验证安装
```

> 直接 `go build` 版本号显示 `dev`；要带版本号：`go build -ldflags "-X main.version=v0.14.6"`。网络受限环境（模块拉不下来）可加 `export GOPROXY=https://goproxy.cn,direct`。官方 `scripts/install.ps1` 硬编码上游仓库，装不了 fork，请用本页两种方式之一。

装好后启动热键：

```bat
cc-clip hotkey <HOST> --enable-autostart   :: 启动 + 开机自启（<HOST> 换成你的 SSH 别名）
cc-clip hotkey --status                    :: 确认运行
```

---

## 3. 快速上手（Windows 热键工作流，本项目主要用法）

```bat
:: 启动热键常驻进程（前台/后台），绑定主机 <HOST>（替换为你的 SSH 别名）
cc-clip hotkey <HOST> --enable-autostart

:: 之后在任何窗口按 alt+shift+v：
::   1) 若剪贴板里有图片 → 上传到远端 → 自动 Ctrl+Shift+V 粘贴出远程路径
::   2) 粘贴完成后后台把原图片放回剪贴板（1 秒内，剪贴板短暂变为路径文本是正常的）
```

热键可用参数：

| 参数 | 默认值 | 说明 |
|---|---|---|
| `[<host>]` | 上次保存的主机 | 目标 SSH 主机（`~/.ssh/config` 别名） |
| `--hotkey` | `alt+shift+v` | 全局热键，格式 `[ctrl+][alt+][shift+][win+]KEY`，支持字母/数字/`f1`~`f24`/`insert`/`delete` |
| `--delay-ms` | `150` | 触发 Ctrl+Shift+V 前的延迟 |
| `--remote-dir` | `~/.cache/cc-clip/uploads` | 远端上传目录 |
| `--no-restore` | 关 | 粘贴后**不**恢复图片剪贴板（剪贴板保留路径文本） |
| `--enable-autostart` / `--disable-autostart` | – | 开机自启（写入 `HKCU\...\Run` + VBS 拉起） |
| `--stop` | – | 停止后台热键进程 |
| `--status` | – | 查看热键状态与当前配置 |

> 热键**不允许**绑定 `ctrl+v`（系统粘贴）或 `ctrl+shift+v`（与合成的粘贴按键冲突），会直接报错。

---

## 4. 常用命令

### 图片上传并粘贴（一次性的 `send`）

```bat
:: 把剪贴板图片上传到 <HOST> 并注入当前窗口（等效热键的单次执行）
cc-clip send <HOST> --paste

:: 不注入，只把远程路径放剪贴板
cc-clip send <HOST>

:: 上传指定文件而非剪贴板
cc-clip send <HOST> C:\path\to\img.png --paste
```

### 状态与体检

```bat
cc-clip version              :: 版本号（当前最新 = v0.14.6）
cc-clip status               :: 各组件状态（daemon/token/服务）
cc-clip doctor               :: 本地健康检查
cc-clip doctor --host <HOST>  :: 端到端体检（经 SSH 检查远端）
cc-clip hotkey --status      :: 热键进程 + 配置详情
```

### 远程 → 本地反向复制（需 SSH 隧道）

```bash
# 在远端主机上：
cat file.txt | cc-clip copy   # 内容原样进入本地剪贴板（无软换行）
```

### 主机与更新

```bat
cc-clip hosts list            :: 本机连接过的远端主机
cc-clip hosts forget <HOST>    :: 停止本地跟踪（不动远端）
cc-clip update --check        :: 检查是否有新版本
```

---

## 5. 支持能力

| 项目 | 说明 |
|---|---|
| 图片格式 | **gif / jpeg / png**（`image.Decode` 嗅探，浏览器复制的 JPEG、网页 CF_HTML 均支持） |
| 复制来源 | 剪贴板图片位图（PNG/JPEG）、CF_HTML 内嵌 `data:image/...;base64`、CF_HDROP 文件/缩略图路径 |
| 上传方式 | SSH 连接池长连接（热键进程内常驻一条 `ssh 主机 helper` 子进程，免握手） |
| 远端清理 | 上传目录内超过 **24 小时**的文件自动 `find -mmin +1440` 删除 |
| 断线自愈 | 上传失败 → 断连重拨一次 → 仍失败回退一次性 SSH；远端重启后自动透明恢复 |
| 剪贴板恢复 | 粘贴后后台恢复图片剪贴板（原生 Win32 写入，~150ms 后执行） |
| 通知 | 托盘气泡提示成功/失败；`CC_CLIP_TIMING=1` 启动可输出各阶段耗时日志 |

---

## 6. 配置文件与日志（Windows）

| 项 | 路径 |
|---|---|
| 热键配置 | `%AppData%\cc-clip\hotkey.json`（host/remote_dir/delay_ms/hotkey/no_restore） |
| 热键日志 | `%LocalAppData%\cc-clip\hotkey.log`（微秒级时间戳，排查延迟用） |
| 进程 PID | `%LocalAppData%\cc-clip\hotkey.pid` |
| 开机自启 | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` → `cc-clip-hotkey`（VBS 拉起） |

远端上传目录：`~/.cache/cc-clip/uploads/`（文件名 `clip-<日期>-<随机>.png|jpg`）。

---

## 7. 常见问题排查

- **剪贴板变成远程路径文本后没有变回图片**：正常现象会恢复；若**一直**不变，多半是终端吃掉了合成按键（少数 Electron 终端会忽略 SendInput），改用 `--no-restore`（剪贴板保留路径文本）。
- **按热键没反应**：`cc-clip hotkey --status` 看进程与配置；日志在 `hotkey.log`。
- **上传明显变慢**：先看 `hotkey.log` 时间戳定位阶段（解码/上传/延迟/注入）；解码耗时与**像素数**成正比，3MB+ 大图端到端约 2s 属正常（v0.14.5 实测 3.8MB 暖态约 0.6s 为解码 ~300ms + 上传 ~50ms + 延迟 150ms + 注入 ~80ms）。
- **远端目录乱/积压**：TTL 只清 24h 以上文件，可手动 `ssh <HOST> 'rm -rf ~/.cache/cc-clip/uploads/*'`。
- **无法连接远端**：确认 `~/.ssh/config` 别名 `<HOST>` 可用、密钥免密登录正常，然后 `cc-clip doctor --host <HOST>`。

---

## 8. 版本演进速览

| 版本 | 内容 |
|---|---|
| v0.12.0 | CF_HTML / CF_HDROP 识别（复制图片判定重写为可测纯函数） |
| v0.13.0 | 远端 uploads 24h TTL 清理 + 解析器抽成纯函数可单测 |
| v0.14.0 | SSH 连接池（消除每次粘贴握手，暖态 ~210ms） |
| v0.14.1 | 异步剪贴板恢复（粘贴先返回，恢复后台执行） |
| v0.14.2 | 原生 Win32 剪贴板写入（弃用 PowerShell，~370ms/次 → 近 0） |
| v0.14.3 | 修复 JPEG 复制恢复失败（弃用硬编码 png.Decode） |
| v0.14.5 | 以上三版 squash 合并发布 |
| v0.14.6 | 修复开机自启被陈旧 stop 哨兵吞掉：自启 VBS 只在自身已轮询时响应 stop |
