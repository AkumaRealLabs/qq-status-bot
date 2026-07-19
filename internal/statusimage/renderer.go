// Package statusimage 原生生成 OneBot 公开状态图片，不依赖浏览器或外部渲染服务。
package statusimage

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"ai-upstream-monitor/internal/domain"
)

const (
	canvasWidth     = 1080
	pageCardLimit   = 10
	pagePadding     = 48
	headerHeight    = 206
	metricHeight    = 112
	groupHeaderSize = 58
	cardHeight      = 88
	groupGap        = 16
	footerHeight    = 56
)

var (
	colorBackground = color.RGBA{239, 244, 248, 255}
	colorHeader     = color.RGBA{28, 42, 68, 255}
	colorSurface    = color.RGBA{255, 255, 255, 255}
	colorText       = color.RGBA{29, 42, 58, 255}
	colorMuted      = color.RGBA{99, 116, 137, 255}
	colorLine       = color.RGBA{219, 226, 234, 255}
	colorNormal     = color.RGBA{24, 131, 93, 255}
	colorDegraded   = color.RGBA{196, 122, 27, 255}
	colorAbnormal   = color.RGBA{211, 61, 61, 255}
	colorPaused     = color.RGBA{126, 106, 157, 255}
	colorSilent     = color.RGBA{105, 120, 139, 255}
	colorNoData     = color.RGBA{135, 149, 166, 255}
)

// Renderer 使用两套 TTF 字体渲染状态卡片。中文由 ChineseFont 绘制，英文和数字由 LatinFont 绘制。
type Renderer struct {
	chineseFont *opentype.Font
	latinFont   *opentype.Font
	Now         func() time.Time
}

// New 从运行时字体路径创建渲染器。
func New(chinesePath, latinPath string) (*Renderer, error) {
	chineseTTF, err := os.ReadFile(chinesePath)
	if err != nil {
		return nil, fmt.Errorf("读取中文字体失败: %w", err)
	}
	latinTTF, err := os.ReadFile(latinPath)
	if err != nil {
		return nil, fmt.Errorf("读取英文字体失败: %w", err)
	}
	return NewFromFontData(chineseTTF, latinTTF)
}

// NewFromFontData 便于测试和受控环境注入字体内容。
func NewFromFontData(chineseTTF, latinTTF []byte) (*Renderer, error) {
	if len(chineseTTF) == 0 || len(latinTTF) == 0 {
		return nil, errors.New("状态图片字体不能为空")
	}
	chineseFont, err := opentype.Parse(chineseTTF)
	if err != nil {
		return nil, fmt.Errorf("解析中文字体失败: %w", err)
	}
	latinFont, err := opentype.Parse(latinTTF)
	if err != nil {
		return nil, fmt.Errorf("解析英文字体失败: %w", err)
	}
	return &Renderer{chineseFont: chineseFont, latinFont: latinFont, Now: time.Now}, nil
}

// Render 根据公开状态投影生成一页或多页 PNG。它只读取公开卡片名称、分组、状态和延迟。
func (r *Renderer) Render(status domain.PublicMonitorStatus) ([][]byte, error) {
	if r == nil || r.chineseFont == nil || r.latinFont == nil {
		return nil, errors.New("状态图片渲染器未初始化")
	}
	typography, err := newTypography(r.chineseFont, r.latinFont)
	if err != nil {
		return nil, err
	}
	defer typography.close()

	pages := buildPages(status)
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	output := make([][]byte, 0, len(pages))
	for i, page := range pages {
		imageData, err := renderPage(typography, status, page, i+1, len(pages), now)
		if err != nil {
			return nil, err
		}
		output = append(output, imageData)
	}
	return output, nil
}

type renderStatus struct {
	label string
	color color.RGBA
}

type renderCard struct {
	name       string
	group      string
	status     renderStatus
	latencyMS  int
	hasLatency bool
}

type groupSummary struct {
	name                              string
	total, normal, degraded, abnormal int
	paused, silent, noData            int
}

type renderGroup struct {
	summary   groupSummary
	continued bool
	cards     []renderCard
}

type renderPageModel struct {
	groups []renderGroup
	cards  int
}

func buildPages(status domain.PublicMonitorStatus) []renderPageModel {
	cards, summaries := publicRenderCards(status.Rows)
	groups := orderedRenderGroups(cards, summaries)
	pages := []renderPageModel{{}}
	for _, group := range groups {
		for cardIndex, card := range group.cards {
			current := &pages[len(pages)-1]
			if current.cards == pageCardLimit {
				pages = append(pages, renderPageModel{})
				current = &pages[len(pages)-1]
			}
			if len(current.groups) == 0 || current.groups[len(current.groups)-1].summary.name != group.summary.name {
				current.groups = append(current.groups, renderGroup{
					summary:   group.summary,
					continued: cardIndex > 0,
				})
			}
			lastGroup := &current.groups[len(current.groups)-1]
			lastGroup.cards = append(lastGroup.cards, card)
			current.cards++
		}
	}
	return pages
}

func orderedRenderGroups(cards []renderCard, summaries map[string]groupSummary) []renderGroup {
	groups := make([]renderGroup, 0, len(summaries))
	byName := make(map[string]int, len(summaries))
	for _, card := range cards {
		index, ok := byName[card.group]
		if !ok {
			index = len(groups)
			byName[card.group] = index
			groups = append(groups, renderGroup{summary: summaries[card.group]})
		}
		groups[index].cards = append(groups[index].cards, card)
	}
	return groups
}

func publicRenderCards(rows []domain.PublicModelCard) ([]renderCard, map[string]groupSummary) {
	cards := make([]renderCard, 0, len(rows))
	summaries := make(map[string]groupSummary)
	for _, row := range rows {
		group := cleanText(row.DisplayGroup, "其他")
		status, latency, hasLatency := publicCardStatus(row)
		card := renderCard{
			name:       cleanText(row.Name, "未命名卡片"),
			group:      group,
			status:     status,
			latencyMS:  latency,
			hasLatency: hasLatency,
		}
		cards = append(cards, card)
		summary := summaries[group]
		summary.name = group
		summary.total++
		switch status.label {
		case "正常":
			summary.normal++
		case "延迟偏高":
			summary.degraded++
		case "暂停":
			summary.paused++
		case "静默":
			summary.silent++
		case "无数据":
			summary.noData++
		default:
			summary.abnormal++
		}
		summaries[group] = summary
	}
	return cards, summaries
}

func publicCardStatus(card domain.PublicModelCard) (renderStatus, int, bool) {
	if card.AutoProbePaused {
		return renderStatus{label: "暂停", color: colorPaused}, 0, false
	}
	if card.ProbeMuted {
		return renderStatus{label: "静默", color: colorSilent}, 0, false
	}
	if len(card.History) == 0 {
		return renderStatus{label: "无数据", color: colorNoData}, 0, false
	}
	last := card.History[len(card.History)-1]
	latency, hasLatency := last.LatencyMS, last.LatencyMS > 0
	switch last.Status {
	case "正常":
		return renderStatus{label: "正常", color: colorNormal}, latency, hasLatency
	case "延迟偏高":
		return renderStatus{label: "延迟偏高", color: colorDegraded}, latency, hasLatency
	default:
		return renderStatus{label: "异常", color: colorAbnormal}, latency, hasLatency
	}
}

func cleanText(value, fallback string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return fallback
	}
	return value
}

func renderPage(t *typography, status domain.PublicMonitorStatus, page renderPageModel, pageNumber, pageCount int, now time.Time) ([]byte, error) {
	pageHeight := headerHeight + 26 + metricHeight + 30 + contentHeight(page) + footerHeight
	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, pageHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(colorBackground), image.Point{}, draw.Src)

	renderHeader(canvas, t, status, page, now)
	renderMetrics(canvas, t, status)

	y := headerHeight + 26 + metricHeight + 30
	for _, group := range page.groups {
		renderGroupHeader(canvas, t, group, y)
		y += groupHeaderSize
		for _, card := range group.cards {
			renderCardRow(canvas, t, card, y)
			y += cardHeight
		}
		y += groupGap
	}
	if len(page.groups) == 0 {
		drawRoundedRect(canvas, image.Rect(pagePadding, y, canvasWidth-pagePadding, y+112), 12, colorSurface)
		drawText(canvas, t, pagePadding+28, y+52, "当前没有公开状态卡片", textStyle{size: 23, color: colorText})
		drawText(canvas, t, pagePadding+28, y+82, "请先在后台将卡片设为公开", textStyle{size: 17, color: colorMuted})
	}

	footerY := pageHeight - 24
	drawText(canvas, t, pagePadding, footerY, "公开状态", textStyle{size: 16, color: colorMuted})
	drawRightText(canvas, t, canvasWidth-pagePadding, footerY, "第 "+strconv.Itoa(pageNumber)+" / "+strconv.Itoa(pageCount)+" 页", textStyle{size: 16, color: colorMuted})

	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, fmt.Errorf("编码状态图片失败: %w", err)
	}
	return output.Bytes(), nil
}

func contentHeight(page renderPageModel) int {
	if len(page.groups) == 0 {
		return 112
	}
	height := 0
	for _, group := range page.groups {
		height += groupHeaderSize + len(group.cards)*cardHeight + groupGap
	}
	return height
}

func renderHeader(dst *image.RGBA, t *typography, status domain.PublicMonitorStatus, page renderPageModel, now time.Time) {
	draw.Draw(dst, image.Rect(0, 0, canvasWidth, headerHeight), image.NewUniform(colorHeader), image.Point{}, draw.Src)
	drawText(dst, t, pagePadding, 64, "公开状态", textStyle{size: 38, color: colorSurface})
	window := strings.ToUpper(cleanText(status.Window, "1H"))
	windowWidth := t.measure(window, 18) + 34
	drawRoundedRect(dst, image.Rect(canvasWidth-pagePadding-windowWidth, 32, canvasWidth-pagePadding, 68), 18, color.RGBA{64, 86, 117, 255})
	drawCenteredText(dst, t, canvasWidth-pagePadding-windowWidth/2, 57, window, textStyle{size: 18, color: colorSurface})

	updated := "更新于 " + now.Format("2006-01-02 15:04")
	drawText(dst, t, pagePadding, 100, updated, textStyle{size: 18, color: color.RGBA{198, 211, 226, 255}})
	summary := totalSummary(status.Rows)
	drawText(dst, t, pagePadding, 154, summary, textStyle{size: 16, color: color.RGBA{220, 228, 237, 255}})
}

func totalSummary(rows []domain.PublicModelCard) string {
	_, groups := publicRenderCards(rows)
	counts := groupSummary{}
	for _, group := range groups {
		counts.normal += group.normal
		counts.degraded += group.degraded
		counts.abnormal += group.abnormal
		counts.paused += group.paused
		counts.silent += group.silent
		counts.noData += group.noData
	}
	return summaryText(counts)
}

func renderMetrics(dst *image.RGBA, t *typography, status domain.PublicMonitorStatus) {
	left := pagePadding
	width := canvasWidth - 2*pagePadding
	metricWidth := (width - 2*16) / 3
	metrics := []struct {
		label string
		value string
		color color.RGBA
	}{
		{label: "请求数", value: strconv.Itoa(status.Requests), color: color.RGBA{29, 110, 140, 255}},
		{label: "成功率", value: strconv.Itoa(int(status.SuccessRate+0.5)) + "%", color: colorNormal},
		{label: "平均延迟", value: latencyText(status.AvgLatency, status.AvgLatency > 0), color: colorDegraded},
	}
	for i, metric := range metrics {
		x := left + i*(metricWidth+16)
		drawRoundedRect(dst, image.Rect(x, headerHeight+26, x+metricWidth, headerHeight+26+metricHeight), 12, colorSurface)
		drawCircle(dst, x+28, headerHeight+54, 7, metric.color)
		drawText(dst, t, x+46, headerHeight+61, metric.label, textStyle{size: 17, color: colorMuted})
		drawText(dst, t, x+24, headerHeight+105, metric.value, textStyle{size: 30, color: colorText})
	}
}

func renderGroupHeader(dst *image.RGBA, t *typography, group renderGroup, y int) {
	title := group.summary.name
	if group.continued {
		title += "（续）"
	}
	drawText(dst, t, pagePadding, y+25, truncateText(t, title, 520, 22), textStyle{size: 22, color: colorText})
	drawRightText(dst, t, canvasWidth-pagePadding, y+25, "共 "+strconv.Itoa(group.summary.total)+" 张", textStyle{size: 17, color: colorMuted})
	drawText(dst, t, pagePadding, y+51, summaryText(group.summary), textStyle{size: 15, color: colorMuted})
}

func renderCardRow(dst *image.RGBA, t *typography, card renderCard, y int) {
	left, right := pagePadding, canvasWidth-pagePadding
	drawRoundedRect(dst, image.Rect(left, y, right, y+cardHeight-8), 10, colorSurface)
	drawRoundedRect(dst, image.Rect(left, y, left+7, y+cardHeight-8), 4, card.status.color)
	drawCircle(dst, left+30, y+30, 7, card.status.color)
	drawText(dst, t, left+50, y+37, truncateText(t, card.name, 600, 21), textStyle{size: 21, color: colorText})
	drawText(dst, t, left+50, y+65, card.status.label, textStyle{size: 17, color: card.status.color})
	latency := latencyText(card.latencyMS, card.hasLatency)
	drawRightText(dst, t, right-28, y+51, latency, textStyle{size: 20, color: colorText})
}

func summaryText(summary groupSummary) string {
	return "正常 " + strconv.Itoa(summary.normal) + "  延迟偏高 " + strconv.Itoa(summary.degraded) + "  异常 " + strconv.Itoa(summary.abnormal) + "  暂停 " + strconv.Itoa(summary.paused) + "  静默 " + strconv.Itoa(summary.silent) + "  无数据 " + strconv.Itoa(summary.noData)
}

func latencyText(latency int, present bool) string {
	if !present {
		return "--"
	}
	return strconv.Itoa(latency) + " ms"
}

type textStyle struct {
	size  float64
	color color.RGBA
}

type faceKey struct {
	size    int
	chinese bool
}

type typography struct {
	chinese *opentype.Font
	latin   *opentype.Font
	faces   map[faceKey]font.Face
}

func newTypography(chinese, latin *opentype.Font) (*typography, error) {
	if chinese == nil || latin == nil {
		return nil, errors.New("状态图片字体未初始化")
	}
	return &typography{chinese: chinese, latin: latin, faces: map[faceKey]font.Face{}}, nil
}

func (t *typography) face(size float64, chinese bool) (font.Face, error) {
	key := faceKey{size: int(size * 100), chinese: chinese}
	if face, ok := t.faces[key]; ok {
		return face, nil
	}
	selected := t.latin
	if chinese {
		selected = t.chinese
	}
	face, err := opentype.NewFace(selected, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	t.faces[key] = face
	return face, nil
}

func (t *typography) close() {
	for _, face := range t.faces {
		_ = face.Close()
	}
}

func (t *typography) measure(text string, size float64) int {
	width := fixed.Int26_6(0)
	for _, r := range text {
		face, err := t.face(size, isChineseRune(r))
		if err != nil {
			continue
		}
		advance, ok := face.GlyphAdvance(r)
		if !ok {
			advance, _ = face.GlyphAdvance('?')
		}
		width += advance
	}
	return int((width + 32) >> 6)
}

func drawText(dst *image.RGBA, t *typography, x, baseline int, text string, style textStyle) {
	pen := fixed.P(x, baseline)
	for _, r := range text {
		face, err := t.face(style.size, isChineseRune(r))
		if err != nil {
			continue
		}
		drawer := &font.Drawer{Dst: dst, Src: image.NewUniform(style.color), Face: face, Dot: pen}
		drawer.DrawString(string(r))
		advance, ok := face.GlyphAdvance(r)
		if !ok {
			advance, _ = face.GlyphAdvance('?')
		}
		pen.X += advance
	}
}

func drawRightText(dst *image.RGBA, t *typography, right, baseline int, text string, style textStyle) {
	drawText(dst, t, right-t.measure(text, style.size), baseline, text, style)
}

func drawCenteredText(dst *image.RGBA, t *typography, center, baseline int, text string, style textStyle) {
	drawText(dst, t, center-t.measure(text, style.size)/2, baseline, text, style)
}

func truncateText(t *typography, value string, maxWidth int, size float64) string {
	if t.measure(value, size) <= maxWidth {
		return value
	}
	const suffix = "..."
	limit := maxWidth - t.measure(suffix, size)
	var out strings.Builder
	for _, r := range value {
		candidate := out.String() + string(r)
		if t.measure(candidate, size) > limit {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + suffix
}

func isChineseRune(r rune) bool {
	return unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}

func drawRoundedRect(dst *image.RGBA, rect image.Rectangle, radius int, fill color.RGBA) {
	if radius <= 0 {
		draw.Draw(dst, rect, image.NewUniform(fill), image.Point{}, draw.Src)
		return
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if roundedRectContains(rect, radius, x, y) {
				dst.SetRGBA(x, y, fill)
			}
		}
	}
}

func roundedRectContains(rect image.Rectangle, radius, x, y int) bool {
	if x >= rect.Min.X+radius && x < rect.Max.X-radius || y >= rect.Min.Y+radius && y < rect.Max.Y-radius {
		return true
	}
	cx, cy := x, y
	if x < rect.Min.X+radius {
		cx = rect.Min.X + radius
	} else if x >= rect.Max.X-radius {
		cx = rect.Max.X - radius - 1
	}
	if y < rect.Min.Y+radius {
		cy = rect.Min.Y + radius
	} else if y >= rect.Max.Y-radius {
		cy = rect.Max.Y - radius - 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

func drawCircle(dst *image.RGBA, centerX, centerY, radius int, fill color.RGBA) {
	for y := centerY - radius; y <= centerY+radius; y++ {
		for x := centerX - radius; x <= centerX+radius; x++ {
			dx, dy := x-centerX, y-centerY
			if dx*dx+dy*dy <= radius*radius {
				dst.SetRGBA(x, y, fill)
			}
		}
	}
}
