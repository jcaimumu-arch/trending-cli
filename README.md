<div align="center">

# 🌐 热榜聚合器

**一条命令看遍全站热点 — 零 API Key，零注册，即开即用（Go 版）**

GitHub · Reddit · 知乎 · HackerNews · V2EX · 微博

</div>

## 快速开始

**前置要求**：安装 [Go 1.21+](https://go.dev/doc/install)

```bash
git clone https://github.com/jcaimumu-arch/trending-cli.git
cd trending-cli
go build -o trending
./trending
```

就是这么简单。

## 功能特性

- **6 个热榜源**：GitHub Trending / Reddit / 知乎 / HackerNews / V2EX / 微博
- **并发抓取**：goroutine 并发请求，6 个源同时抓取
- **自动代理探测**：启动时自动检测本地 Clash 代理（7897/7890/7891），Clash 开着就能直连海外站点
- **OSC 8 终端超链接**：Ctrl/Cmd+click 标题即可在浏览器中打开
- **中文宽度对齐**：使用 `go-runewidth` 正确处理中日韩双宽字符，表格列对齐
- **双列自适应布局**：宽终端 (≥110 列) 双列并排，窄终端单列
- **零依赖配置**：零 API Key、零注册、纯公开接口

## 数据源

| 源 | 图标 | 说明 |
|----|------|------|
| **GitHub Trending** | `[star]` | 今日热门仓库，含星数和语言 |
| **Reddit** | `[R]` | r/popular 全球热帖 |
| **知乎** | `[Zh]` | 知乎发现页热门问题 |
| **Hacker News** | `[HN]` | Firebase 公开 API，硅谷极客头条 |
| **V2EX** | `[V]` | 中文技术社区热门主题 |
| **微博** | `[Wb]` | 实时热搜榜 |

全部零 Key、零认证、纯公开接口。

## 界面预览

```
╔════════════════════════════════════════════════════════════════════════╗
║                                                                        ║
║  [Net] 热榜聚合    70 条 · 19:30                                        ║
║                                                                        ║
║  ╭──────────────────────────────────────────╮  ╭──────────────────────╮ ║
║  │ [star] GitHub                            │  │ [HN] HackerNews      │ ║
║  │                                          │  │                      │ ║
║  │ 1  user/repo                  ★ 1.2k     │  │ 1  Title       369   │ ║
║  │ 2  another/repo               ★ 800       │  │ 2  Title       549   │ ║
║  │ ...                                      │  │ ...                  │ ║
║  ╰──────────────────────────────────────────╯  ╰──────────────────────╯ ║
║                                                                        ║
║   [19:30]  点击标题（Ctrl/Cmd+click）在浏览器中打开  |  70 条            ║
║                                                                        ║
╚════════════════════════════════════════════════════════════════════════╝
```

## 命令行参数

```bash
./trending             # 抓取并展示全部 6 个源的热榜
./trending --version    # 查看版本号
./trending --proxy http://127.0.0.1:7897       # 手动指定 HTTP 代理
./trending --proxy socks5://127.0.0.1:1080    # 手动指定 SOCKS5 代理
./trending --timeout 60                         # 调整请求超时秒数（默认 30）
```

### 代理支持

程序会按以下优先级自动选择代理：

1. `--proxy` 命令行参数（最高优先级）
2. `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` 环境变量
3. **自动探测本地 Clash 代理**（无需任何配置）
4. 直连（不走代理）

**自动探测**：启动时会用极短超时（150ms）探测本地 Clash 常用端口
（`127.0.0.1:7897` / `7890` / `7891`），哪个通就用哪个。
探测到代理时会显示提示：

```
加载各大热榜…（探测到本地代理 http://127.0.0.1:7897）
```

Clash 没开时自动回退直连，不影响使用。

> 海外源（GitHub / HackerNews / Reddit / V2EX）在国内网络下建议配置代理。

## 项目结构

```
trending-cli/
├── fetch.go        # 数据模型 + HTTP 客户端 + 6 个源的抓取函数 + 代理探测
├── render.go       # 终端渲染（面板 / 双列布局 / 颜色 / OSC8 超链接）+ 程序入口 main()
├── go.mod          # Go 模块定义
├── go.sum          # 依赖校验
├── trending.sh     # 便捷启动脚本（调用编译好的二进制）
├── .replit         # Replit 运行配置
├── replit.nix      # Replit Nix 依赖
└── README.md       # 本文件
```

## 技术栈

| 用途 | 依赖 |
|------|------|
| 网络请求 | `net/http`（标准库，goroutine 并发） |
| 代理支持 | HTTP/HTTPS 代理（`http.Transport.Proxy`）+ SOCKS5（`golang.org/x/net/proxy`） |
| HTML 解析 | `github.com/PuerkitoBio/goquery` |
| 终端渲染 | 手动 ANSI 转义 + `github.com/charmbracelet/lipgloss`（颜色） |
| 宽度对齐 | `github.com/mattn/go-runewidth`（中日韩双宽字符） |
| 终端尺寸 | `golang.org/x/term` |

## 构建

```bash
# 编译
go build -o trending

# 交叉编译（可选）
GOOS=darwin GOARCH=arm64 go build -o trending-darwin    # macOS Apple Silicon
GOOS=linux  GOARCH=amd64 go build -o trending-linux     # Linux x86_64
GOOS=windows GOARCH=amd64 go build -o trending.exe      # Windows
```

## 免责声明

本工具仅供学习交流，所有数据来自各大平台公开接口，请合理使用。
