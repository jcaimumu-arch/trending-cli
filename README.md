<div align="center">

# 🌐 热榜聚合器

**一条命令看遍全站热点 — 零 API Key，零注册，即开即用（Go 版）**

</div>

## 快速开始

```bash
go build -o trending
./trending
```

就是这么简单。

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
║  [Net] 热榜聚合    45 条 · 19:30                                        ║
║                                                                        ║
║  ╭──────────────────────────────────────────╮  ╭──────────────────────╮ ║
║  │ [star] GitHub                            │  │ [HN] HackerNews      │ ║
║  │                                          │  │                      │ ║
║  │ 1  user/repo                  ★ 1.2k     │  │ 1  Title       369   │ ║
║  │ 2  another/repo               ★ 800       │  │ 2  Title       549   │ ║
║  │ ...                                      │  │ ...                  │ ║
║  ╰──────────────────────────────────────────╯  ╰──────────────────────╯ ║
║                                                                        ║
║   [19:30]  点击标题（Ctrl/Cmd+click）在浏览器中打开  |  45 条            ║
║                                                                        ║
╚════════════════════════════════════════════════════════════════════════╝
```

- 宽终端 (≥110 列) 自动双列布局，窄终端单列。
- 标题支持 OSC 8 终端超链接，Ctrl/Cmd+click 即可在浏览器中打开。
- 中文宽度感知对齐（使用 `go-runewidth`）。
- 支持通过 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量走代理。

## 命令行参数

```bash
./trending             # 抓取并展示全部 6 个源的热榜
./trending --version   # 查看版本
./trending --proxy http://127.0.0.1:7897      # 通过 HTTP 代理抓取（海外源推荐）
./trending --proxy socks5://127.0.0.1:1080    # 通过 SOCKS5 代理抓取
./trending --timeout 60                       # 调整请求超时秒数（默认 30）
```

也支持标准环境变量（优先级低于 `--proxy`）：

```bash
export HTTPS_PROXY=http://127.0.0.1:7897
./trending
```

> 海外源（GitHub / HackerNews / Reddit / V2EX）在国内网络下建议配置代理。

## 项目结构

```
trending-cli/
├── main.go         # 入口与并发抓取
├── fetch.go        # 数据模型 + 6 个源的抓取函数
├── render.go       # 终端渲染（面板 / 表格 / 颜色 / OSC8 超链接）
├── go.mod          # Go 模块定义
├── trending.sh     # 便捷启动脚本
└── README.md       # 本文件
```

## 技术栈

- **语言**: Go 1.21+
- **网络请求**: `net/http`（标准库，goroutine 并发）
- **代理支持**: HTTP / HTTPS / SOCKS5（`golang.org/x/net/proxy`）
- **HTML 解析**: `github.com/PuerkitoBio/goquery`
- **终端渲染**: 手动 ANSI + `github.com/charmbracelet/lipgloss`（颜色）
- **宽度对齐**: `github.com/mattn/go-runewidth`（中日韩双宽字符）
- **终端尺寸**: `golang.org/x/term`

## 免责声明

本工具仅供学习交流，所有数据来自各大平台公开接口，请合理使用。
