# ailock

[English](README.md) | 简体中文

一个极小的常驻守护进程：**当代理 / VPN 没有运行时，把美国 AI 服务商的域名
全部黑洞**；代理一恢复，立即解除。支持 Linux、Windows、macOS 和 OpenWrt
（每个平台一个 Go 静态二进制，无需运行时，无 CGO）。命令行输出会根据系统
语言自动切换中文 / 英文。

它解决的问题：代理停止（或崩溃循环）时，客户端会回落到直连，把你的真实
IP 暴露给 OpenAI、Anthropic、Google、Microsoft、xAI 等服务商——轻则限流，
重则账号被标记、封禁。ailock 让这些域名**直接失败**而不是**泄露 IP**。
机器上的其他一切照常工作：只有列表里的域名受影响，且只在代理断开期间。

## 工作原理

- 健康探测（代理客户端的本地端口、HTTP 端点或 shell 命令）每 2 秒执行一次。
- 首次失败 → 把屏蔽列表里所有域名的 `0.0.0.0` / `::` 记录写入 hosts 文件的
  **标记区域**，并刷新 DNS 缓存。
- 连续 2 次成功 → 移除该区域，再次刷新。
- **故障关闭（fail closed）**：启动时先屏蔽再探测（代理健康时约 2 秒后自动
  解除）；守护进程停止时保持当前状态（`ailock unblock` 是手动逃生通道）；
  配置损坏时保持屏蔽。
- 在 OpenWrt（或任何运行 dnsmasq 的地方）上，可选的 `dnsmasq_conf_dir` 会
  额外维护 `address=/域名/` 通配规则，让整个局域网获得真正的子域名通配覆盖。

代理正常运行时，DNS / 路由完全不受影响——hosts 区域根本不存在。

## 安装

自行编译（`build.sh`）或从 [发布页][releases] 下载对应二进制：

    bash build.sh          # → dist/ailock-{linux,windows,darwin}-{amd64,arm64,...}

然后**以管理员身份**（sudo / Administrator）执行：

    ./ailock install

它会：把二进制复制到 `/usr/local/bin`（OpenWrt 为 `/usr/bin`）、在缺失时写入
`/etc/ailock/config.json` 和 `/etc/ailock/blocklist`（Windows 为
`%ProgramData%\ailock\`）、注册并启动系统服务（systemd / procd / launchd /
Windows 服务）。**安装前后请把配置里的 `check`/`checks` 改成你自己的代理。**

### 下载 release 二进制安装

从[发布页][releases] 选择对应架构（`uname -m`：`x86_64` → amd64，
`aarch64` → arm64），以 root/管理员运行其 `install` 命令——它会自我安装，
下载的文件之后可以删掉。

Linux：

    curl -LO https://github.com/shenghsi/ailock/releases/latest/download/ailock-linux-amd64
    chmod +x ailock-linux-amd64
    sudo ./ailock-linux-amd64 install

OpenWrt：下载或 `scp` 对应架构（`arm`、`mips`、`mipsle`）后执行
`./ailock-linux-<架构> install` —— 安装到 `/usr/bin`、注册 procd 服务，
并默认开启 `dnsmasq_conf_dir=/tmp/dnsmasq.d` 实现全局域网通配屏蔽。

macOS（Apple Silicon；Intel 用 `-amd64`）：

    curl -LO https://github.com/shenghsi/ailock/releases/latest/download/ailock-darwin-arm64
    chmod +x ailock-darwin-arm64
    sudo ./ailock-darwin-arm64 install
    # 浏览器下载的二进制需先去掉隔离标志：
    # xattr -d com.apple.quarantine ailock-darwin-arm64

Windows：下载 `ailock-windows-amd64.exe`，改名为 `ailock.exe`，在管理员
PowerShell 中运行 `.\ailock.exe install`（SmartScreen 提示时选*更多信息 →
仍要运行*——二进制未签名）。

安装后**把探测指向你的代理**：编辑 `/etc/ailock/config.json`（Windows 为
`%ProgramData%\ailock\config.json`，见下表），然后重启服务使其生效：

    sudo systemctl restart ailock                        # Linux
    /etc/init.d/ailock restart                           # OpenWrt
    sudo launchctl kickstart -k system/com.ailock.daemon # macOS
    sc stop ailock && sc start ailock                     # Windows（管理员）

在探测目标真正对应你的代理之前，ailock 会一直保持**屏蔽**（设计如此，故障
关闭）——`ailock status` 可以看到当前处于哪种状态。

### 更新

    ailock update --check   # 检查是否有新版本
    sudo ailock update      # 下载、校验、替换二进制并重启服务

update 命令会从 GitHub release 选择匹配当前系统/架构的文件，按 release
附带的 SHA256SUMS 校验后，原子地替换已安装的二进制（服务运行中也可；
Windows 上会先把旧 exe 改名为 `ailock.exe.old`），然后重启服务。配置与
屏蔽列表不会被改动。如果直连 GitHub 不通，请先设置 `https_proxy`——下载
遵循代理环境变量。

手动更新依然可用：下载新版 release 二进制再执行一次它的 `install`（原子
替换、保留配置/屏蔽列表、重启服务）。新版本可能更新了默认屏蔽列表：你的
列表会被保留；如需采用新默认，参照仓库里的 `blocklist` 文件，删除
`/etc/ailock/blocklist` 后重新生成。

卸载：`ailock uninstall`（保留配置和屏蔽列表）。
停止服务：`ailock stop`（需 root/管理员）。停止不会改动 hosts 文件——
已生效的屏蔽会一直保留，直到执行 `ailock unblock`（故障关闭）。
重新启动用上面的重启命令（把 restart 换成 start）。
手动覆盖 / 测试：`ailock block` / `ailock unblock` / `ailock status`。

## 配置（`/etc/ailock/config.json`）

```json
{
  "checks": [
    { "kind": "tcp",  "target": "127.0.0.1:10808", "timeout_sec": 1 },
    { "kind": "tcp",  "target": "127.0.0.1:7890",  "timeout_sec": 1 },
    { "kind": "exec", "target": "tailscale status", "timeout_sec": 2 }
  ],
  "lang": "auto",
  "interval_sec": 2,
  "unblock_after": 2,
  "hosts_file": "",
  "blocklist_file": "",
  "dnsmasq_conf_dir": ""
}
```

- `lang`——命令行/日志语言：`auto`（默认）跟随系统语言；OpenWrt 没有
  locale 环境变量，会自动跟随 LuCI 界面语言；也可设为 `en` 或 `zh` 强制
  指定。
- `check`——单个探测（简单场景）。`checks`——**任一成功即算代理在线**的
  探测列表，适合一台机器跑多个客户端的情况。两者可以同时使用。
- 探测类型：`tcp`（TCP 连接）、`http`（GET，2xx 即在线）、`exec`（shell
  命令，退出码 0 即在线；Unix 用 `sh -c`，Windows 用 `cmd /C`）。
- `dnsmasq_conf_dir`：如 OpenWrt 上的 `/tmp/dnsmasq.d`。

### 各客户端探测什么

| 客户端 | check |
| --- | --- |
| sing-box / v2rayN（默认混合端口） | `tcp 127.0.0.1:10808` |
| 局域网内自建代理服务器 | `tcp 192.168.1.2:10808` |
| Clash / Mihomo / Clash Verge | `tcp 127.0.0.1:7890`（Verge 常为 `7897`） |
| sing-box 官方示例配置 | `tcp 127.0.0.1:2080` |
| Xray / V2Ray 内核 | `tcp 127.0.0.1:1080` 或 `10808` |
| Shadowsocks 客户端 | `tcp 127.0.0.1:1080` |
| clash 系 API 存活检测 | `http http://127.0.0.1:9090/version` |
| Tailscale 在线 | `exec tailscale status`（或 `ip link show tailscale0`） |
| WireGuard 在线 | `exec wg show interfaces`（有输出即在线） |

优先探测**代理客户端的本地端口**：网卡/接口在线（如 `tailscale0`）并不代表
访问 AI 服务商的流量真的走了代理——只有当代理绑定在该隧道上时，才把接口
检测作为 any-of 的补充项。

## 屏蔽列表（`/etc/ailock/blocklist`）

纯文本，每行一个裸域名，支持 `[分区]` 头和 `#` 注释。当前分区：
`[openai]`、`[anthropic]`、`[google]`（覆盖很宽：账号、YouTube、Gmail、
API、静态 CDN）、`[ms-copilot]`（含 GitHub Copilot）、`[xai-grok]`、
`[cursor]`、`[doh-bypass]`（知名 DoH 端点，让开了安全 DNS 的浏览器回落到
系统解析、从而命中黑洞）。

运行中的守护进程持有启动时加载的副本：修改后需重启服务生效（手动
`ailock block` / `ailock unblock` 会立即重新读取）。文件缺失或损坏时使用
**内置副本**——损坏的屏蔽列表绝不会让熔断保护悄悄失效。

## 中国用户注意事项

- **为什么需要它**：直连时很多 AI 端点其实能通（Cloudflare 前置的域名尤其
  如此），一次成功的直连就足以让服务商记录到你的真实 IP。断代理解释不了
  账号为什么被风控——ailock 把"回落直连"变成"直接失败"。
- **doh.pub 不在 doh-bypass 屏蔽列表里**。如果你的网络把某个国内 DoH
  （如 doh.pub）作为代理断开时的直连解析通道，千万别把它加进屏蔽——否则
  代理一断，连普通网站的解析都会出问题。
- **OpenWrt 是性价比最高的部署点**：一台路由器 + dnsmasq 通配规则，全家
  所有设备（包括手机）都被覆盖，无需逐台安装。
- **日志**：Linux 看 `journalctl -u ailock -f`；OpenWrt 看 `logread`。
  命令行输出已支持中文（跟随系统语言，Windows 跟随系统界面语言）。

## 客观局限

- **泄露窗口**：代理断掉后的一个 `interval_sec` 内，新解析仍可能直连成功；
  已建立的连接不会被切断。默认约 2 秒。
- **hosts 不支持通配**：动态子域名 CDN（googlevideo、googleusercontent）
  无法穷举——入口域名被屏蔽后客户端会尽早失败。dnsmasq 模式（OpenWrt）
  没有这个缺口。
- **硬编码 DoH 的应用**会绕过 hosts 文件，除非其解析端点也被黑洞
  （`doh-bypass` 分区覆盖了常见的那些）。
- **已有连接不会被切断**——屏蔽只影响新解析。
- 有意不做防火墙 IP 段屏蔽：服务商域名都挂在 CDN 后面，IP 列表既会误伤
  又会漏防。

## 疑难解答

- **二进制未签名**：Windows SmartScreen 首次运行会警告（*更多信息 → 仍要
  运行*）；macOS 会隔离浏览器下载的文件——`xattr -d com.apple.quarantine
  <文件>`（用 `curl` 下载则不会带标志）。Linux 只需 `chmod +x`。
- **日志在哪**：Linux `journalctl -u ailock -f`；OpenWrt
  `logread -f | grep ailock`；macOS `/var/log/ailock.log`；Windows
  `%ProgramData%\ailock\ailock.log`（服务无控制台，stderr 重定向到此）。
- **配置只在启动时读取一次**。修改 `config.json`/`blocklist` 后需重启服务
  （命令见"下载 release 二进制安装"一节）。手动 `ailock block` / `unblock`
  / `status` 总是重新读取。
- **`dnsmasq_conf_dir` 放在 tmpfs 里没问题。** OpenWrt 默认的
  `/tmp/dnsmasq.d` 在内存中、重启即清空——这正符合 OpenWrt 每次开机重建
  dnsmasq 配置的方式（生成的配置本身就在 tmpfs；`/etc` 里的 UCI 才是持久
  真相）。ailock 每次启动都会在故障关闭阶段重建该文件，目录不存在也会自行
  创建，屏蔽期间文件丢失会自动补写。无需备份任何 /tmp 内容；持久状态都在
  `/etc/ailock`。
- **一直屏蔽、解除不了？** 几乎总是探测目标不对：`ailock check` 会执行一次
  你配置的探测并打印错误。探测通过之前保持屏蔽是故障关闭设计，不是 bug。
  `ailock unblock` 是逃生通道。
- **开机后屏蔽几秒**即使代理正常：守护进程先屏蔽再首次探测（约 2 秒解除）。
- **Windows**：凡涉及 hosts 文件或服务的命令，请用管理员权限运行。

## 验证安装

Linux/systemd 主机上，下面整套流程是自动化的：

    sudo ./scripts/selftest.sh [代理服务名] [测试域名]
    # 例：sudo ./scripts/selftest.sh proxyctl youtube.com

它会停掉你的代理服务，断言屏蔽生效且普通联网不受影响，再启动代理并
断言恢复；无论成败，代理服务最终都会被恢复。

1. `ailock status` —— 探测正常、未屏蔽、代理在线。
2. 停掉代理（如 `sudo systemctl stop <代理>`）→ 约 3 秒内 `ailock status`
   显示已屏蔽；`ping chatgpt.com` / `getent hosts chatgpt.com` 解析为
   `0.0.0.0`。**其他网站不受影响**（curl 一个国内站点验证）——普通联网必须
   正常。
3. 启动代理 → 约 4-6 秒内解除屏蔽，AI 站点恢复正常解析。

## 开发

    go build ./...     # 编译
    go test ./...      # 单元测试（也请跑 -race）
    go vet ./...
    gofmt -l .         # 必须无输出
    bash build.sh      # 交叉编译所有平台 → dist/

平台相关代码在构建标签后面（`internal/platform`、`internal/svcwin`）；
改动它们之前请运行 `GOOS=windows go vet ./...` 和 `GOOS=darwin go vet
./...`——纯 Linux 构建不会编译这些带标签的文件。CI 会在 Linux、Windows、
macOS 三端跑测试。

**所有用户可见字符串必须双语**：用 `i18n.T("english", "中文")` 包裹（见
`internal/i18n`），中文用户会自动看到中文。

## 参与贡献

见 [CONTRIBUTING.md](CONTRIBUTING.md)（英文）——包含最常见的贡献方式：
向屏蔽列表添加域名。中文 issue 和 PR 同样欢迎。

## 许可证

[MIT](LICENSE)

[releases]: https://github.com/shenghsi/ailock/releases
