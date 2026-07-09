package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// ── 颜色样式 ──────────────────────────────────────────────────────

// osc8 生成 OSC 8 终端超链接（Ctrl/Cmd+click 在浏览器中打开）
func osc8(text, url string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

// dispWidth 返回字符串在终端中的显示宽度（中文占 2 列）
func dispWidth(s string) int {
	return runewidth.StringWidth(s)
}

// truncWidth 按显示宽度截断字符串，末尾补省略号
func truncWidth(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxW {
		return s
	}
	return runewidth.Truncate(s, maxW, "…")
}

// padWidth 将字符串右侧填充空格至指定显示宽度
func padWidth(s string, w int) string {
	dw := runewidth.StringWidth(s)
	if dw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-dw)
}

// rankColor 按排名返回颜色
func rankColor(rank int) string {
	switch {
	case rank <= 3:
		return "1" // 红
	case rank <= 5:
		return "3" // 黄
	default:
		return "15" // 亮白
	}
}

// makeSourcePanel 为一个源生成 Panel
// contentWidth = panel 内部内容宽度（不含 border）
func makeSourcePanel(source string, items []Item, emoji, color string, contentWidth int) string {
	title := fmt.Sprintf("%s %s", emoji, source)
	titleStyled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(title)

	if contentWidth < 30 {
		contentWidth = 30
	}

	if len(items) == 0 {
		body := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("加载失败或暂无数据")
		// 手动构建面板，避免 lipgloss 对含 OSC8 的行做宽度换行
		return manualPanel(titleStyled, []string{body}, contentWidth, color)
	}

	// 列宽分配：rank(3) + gap(1) + title(flex) + gap(1) + heat(12)
	rankW := 3
	heatW := 12
	gap := 1
	titleW := contentWidth - rankW - gap - heatW - gap
	if titleW < 10 {
		titleW = 10
	}

	var rows []string
	maxItems := 10
	if len(items) < maxItems {
		maxItems = len(items)
	}

	for i := 0; i < maxItems; i++ {
		it := items[i]

		// 排名
		rankStr := fmt.Sprintf("%2d", it.Rank)
		rankStyled := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(rankColor(it.Rank))).
			Render(rankStr)
		rankPadded := padWidth(rankStyled, rankW)

		// 标题（截断 + 超链接）
		// 注意：OSC8 超链接含不可见转义序列，padWidth 不能直接测量它，
		// 需先用纯文本宽度计算所需空格数，再拼到超链接字符串后面。
		titleText := truncWidth(it.Title, titleW)
		titleUsedW := dispWidth(titleText)
		titleWithLink := osc8(titleText, it.URL)
		titlePadded := titleWithLink + strings.Repeat(" ", titleW-titleUsedW)

		// 热度（按显示宽度截断 + 填充）
		heatText := truncWidth(it.Heat, heatW)
		heatPadded := padWidth(heatText, heatW)

		// 组装第一行
		line1 := rankPadded + strings.Repeat(" ", gap) + titlePadded + strings.Repeat(" ", gap) + heatPadded

		// 描述行
		if it.Desc != "" {
			d := truncWidth(it.Desc, titleW)
			descStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  " + d)
			indent := strings.Repeat(" ", rankW+gap)
			line2 := indent + descStyled
			rows = append(rows, line1+"\n"+line2)
		} else {
			rows = append(rows, line1)
		}
	}

	return manualPanel(titleStyled, rows, contentWidth, color)
}

// manualPanel 手动绘制圆角边框面板。
// 关键点：lipgloss 的 Width() 会按字符串字节/字符测量，遇到含 OSC8 转义序列
// 的行会误判宽度并触发换行；这里每行手动填充至 contentWidth 再加边框，
// 完全规避 lipgloss 对内容做任何宽度处理。
func manualPanel(title string, body []string, contentWidth int, borderColor string) string {
	colorize := func(s string) string {
		return "\x1b[" + ansiColorFG(borderColor) + "m" + s + "\x1b[0m"
	}
	// 顶部/底部边框
	top := colorize("╭") + strings.Repeat("─", contentWidth) + colorize("╮")
	bot := colorize("╰") + strings.Repeat("─", contentWidth) + colorize("╯")
	left := colorize("│")
	right := colorize("│")

	// 标题行：左侧标题 + 右侧空格填充
	titleLine := padPlainWidth(title, contentWidth)

	var b strings.Builder
	b.WriteString(top + "\n")
	b.WriteString(left + titleLine + right + "\n")
	b.WriteString(left + strings.Repeat(" ", contentWidth) + right + "\n")
	for _, r := range body {
		// 每行可能多行（含描述），逐行处理
		for _, sub := range strings.Split(r, "\n") {
			b.WriteString(left + padPlainWidth(sub, contentWidth) + right + "\n")
		}
	}
	b.WriteString(bot)
	return b.String()
}

// ansiColorFG 将 lipgloss 颜色名/数字转换为 ANSI 前景色 SGR 参数
func ansiColorFG(c string) string {
	switch c {
	case "1":
		return "31"
	case "2":
		return "32"
	case "3":
		return "33"
	case "4":
		return "34"
	case "5":
		return "35"
	case "6":
		return "36"
	case "15":
		return "97"
	default:
		return "37"
	}
}

// padPlainWidth 将行右侧填充空格至 contentWidth 显示宽度。
// 注意：行内可能含 ANSI 颜色/OSC8 转义序列，这些不可见，
// 需用 runewidth 测量「实际显示宽度」再补足空格。
func padPlainWidth(s string, contentWidth int) string {
	// 先去除所有 ANSI 转义序列以测量真实显示宽度
	plain := stripAnsi(s)
	dw := runewidth.StringWidth(plain)
	if dw >= contentWidth {
		return s
	}
	return s + strings.Repeat(" ", contentWidth-dw)
}

// stripAnsi 移除字符串中的 ANSI 转义序列（颜色 / OSC8 等）
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\][^\x1b]*\x1b\\\\|\x1b\\][^\x07]*\x07")

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// buildDisplay 组装全部面板
func buildDisplay(github, reddit, zhihu, hackernews, v2ex, weibo []Item, width int, ts string) string {
	type src struct {
		name  string
		items []Item
		emoji string
		color string
	}
	sources := []src{
		{"GitHub", github, "[star]", "2"},
		{"Reddit", reddit, "[R]", "5"},
		{"知乎", zhihu, "[Zh]", "6"},
		{"HackerNews", hackernews, "[HN]", "3"},
		{"V2EX", v2ex, "[V]", "4"},
		{"微博", weibo, "[Wb]", "1"},
	}

	twoCol := width >= 110

	// 计算内部面板内容宽度（不含 border）
	// 外框: border(2) + padding(4) = 6
	// 两列时: (width - 6 - gap) / 2，再减去 panel border(2)
	var panelContentW int
	if twoCol {
		gapBetween := 2
		panelContentW = (width-6-gapBetween)/2 - 2
	} else {
		panelContentW = width - 6 - 2
	}
	if panelContentW < 30 {
		panelContentW = 30
	}

	var panels []string
	for _, s := range sources {
		panels = append(panels, makeSourcePanel(s.name, s.items, s.emoji, s.color, panelContentW))
	}
	// 记录面板总宽度（含 border）供 joinPanelRows 对齐使用
	panelTotalWidth = panelContentW + 2

	var content string
	if twoCol && len(panels) >= 3 {
		mid := (len(panels) + 1) / 2
		content = joinPanelRows(panels[:mid], panels[mid:], gapBetweenPanels)
	} else {
		content = strings.Join(panels, "\n")
	}

	total := len(github) + len(reddit) + len(hackernews) + len(v2ex) + len(weibo) + len(zhihu)
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		fmt.Sprintf("  [%s]  点击标题（Ctrl/Cmd+click）在浏览器中打开  |  %d 条", ts, total),
	)

	title := lipgloss.NewStyle().Bold(true).Render("[Net] 热榜聚合")
	subtitle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		fmt.Sprintf(" %d 条 · %s ", total, ts),
	)
	header := title + "   " + subtitle

	outerContent := header + "\n\n" + content + "\n\n" + footer
	return manualOuterPanel(outerContent, width, "15")
}

const gapBetweenPanels = 2

// joinPanelRows 将左右两列面板逐行拼接（手动实现，避免 lipgloss 宽度计算问题）
func joinPanelRows(left, right []string, gap int) string {
	leftLines := strings.Split(strings.Join(left, "\n"), "\n")
	rightLines := strings.Split(strings.Join(right, "\n"), "\n")
	// 补齐行数
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}
	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}
	gapStr := strings.Repeat(" ", gap)
	var b strings.Builder
	for i := 0; i < maxLines; i++ {
		l := leftLines[i]
		r := rightLines[i]
		// 计算左侧行的显示宽度以填充至对齐
		lw := dispWidth(stripAnsi(l))
		// 面板宽度 = contentW + 2(border)。这里直接用左侧已有宽度
		b.WriteString(l)
		if lw < panelTotalWidth {
			b.WriteString(strings.Repeat(" ", panelTotalWidth-lw))
		}
		b.WriteString(gapStr)
		b.WriteString(r)
		if i < maxLines-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// panelTotalWidth 用于 joinPanelRows 对齐；在 makeSourcePanel 后设置
var panelTotalWidth int

// manualOuterPanel 手动绘制外层双线边框面板
func manualOuterPanel(content string, width int, borderColor string) string {
	contentWidth := width - 2 - 4 // border(2) + padding(4)
	if contentWidth < 30 {
		contentWidth = 30
	}
	colorize := func(s string) string {
		return "\x1b[" + ansiColorFG(borderColor) + "m" + s + "\x1b[0m"
	}
	top := colorize("╔") + strings.Repeat("═", contentWidth) + colorize("╗")
	bot := colorize("╚") + strings.Repeat("═", contentWidth) + colorize("╝")
	left := colorize("║")
	right := colorize("║")
	// padding 上下各 1 行、左右各 2 空格
	padLine := strings.Repeat(" ", contentWidth)
	leftPad := strings.Repeat(" ", 2)

	var b strings.Builder
	b.WriteString(top + "\n")
	b.WriteString(left + padLine + right + "\n") // 上 padding
	for _, line := range strings.Split(content, "\n") {
		b.WriteString(left + leftPad + padPlainWidth(line, contentWidth-2) + leftPad + right + "\n")
	}
	b.WriteString(left + padLine + right + "\n") // 下 padding
	b.WriteString(bot)
	return b.String()
}

// ── 主入口 ──────────────────────────────────────────────────────

func main() {
	var (
		showVersion bool
		proxyFlag   string
		timeoutSec  int
		saveMode    bool
	)
	flag.BoolVar(&showVersion, "version", false, "显示版本号")
	flag.StringVar(&proxyFlag, "proxy", "", "HTTP/SOCKS5 代理地址，例如 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080")
	flag.IntVar(&timeoutSec, "timeout", 30, "请求超时秒数")
	flag.BoolVar(&saveMode, "save", true, "将抓取结果存储到本地（~/.trending-cli/data/），含正文摘要，自动去重（默认开启）")
	flag.Parse()

	if showVersion {
		fmt.Printf("trending v%s\n", version)
		return
	}

	if proxyFlag != "" {
		proxyAddr = proxyFlag
	} else if envProxy := os.Getenv("HTTP_PROXY"); envProxy == "" && os.Getenv("HTTPS_PROXY") == "" && os.Getenv("ALL_PROXY") == "" {
		// 未显式指定代理时，自动探测本地 Clash 代理
		detectedProxy = detectLocalProxy()
	}
	if timeoutSec > 0 {
		httpTimeout = time.Duration(timeoutSec) * time.Second
	}

	loadingMsg := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")).Render("加载各大热榜…")
	if p := proxyURL(); p != "" && proxyFlag == "" {
		// 显示探测到的代理（让用户知道在走代理）
		loadingMsg += lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("（探测到本地代理 " + p + "）")
	}
	fmt.Println(loadingMsg)

	github, reddit, zhihu, hackernews, v2ex, weibo := fetchAll(15)

	// --save：存储到本地 + 抓取正文摘要 + 去重
	if saveMode {
		count, err := saveItems(github, reddit, zhihu, hackernews, v2ex, weibo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "存储失败: %v\n", err)
		} else if count > 0 {
			hint := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(
				fmt.Sprintf("  [已存储 %d 条到 ~/.trending-cli/data/]", count),
			)
			fmt.Println(hint)
		} else {
			hint := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
				"  [本次无新条目，全部已存储过]",
			)
			fmt.Println(hint)
		}
	}

	ts := time.Now().Format("15:04:05")
	width := termWidth()
	out := buildDisplay(github, reddit, zhihu, hackernews, v2ex, weibo, width, ts)
	fmt.Println(out)
}
