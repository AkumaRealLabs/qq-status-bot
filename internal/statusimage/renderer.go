package statusimage

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"qq-status-bot/internal/statusapi"
)

const (
	imageWidth  = 1280
	margin      = 48
	cardGap     = 18
	cardHeight  = 365
	rowGap      = 18
	renderScale = 4
)

var (
	//go:embed assets/NotoSansCJK-Regular.ttc
	notoSansCJK []byte

	background = hex("#F5F7FA")
	surface    = hex("#FFFFFF")
	textMain   = hex("#18212F")
	textMuted  = hex("#596575")
	border     = hex("#E1E6EC")
	chartGrid  = hex("#E7EBF0")
)

type Renderer struct{}

type painter struct {
	img   *image.RGBA
	faces map[int]font.Face
	scale int
}

type statusStyle struct {
	name   string
	color  color.RGBA
	soft   color.RGBA
	header string
}

func (Renderer) Render(page statusapi.StatusPage) ([]byte, error) {
	height := imageHeight(page.Groups)
	if height > 4096 {
		return nil, errors.New("状态图内容过多，生成高度超过 4096px")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, imageWidth*renderScale, height*renderScale))
	stddraw.Draw(canvas, canvas.Bounds(), image.NewUniform(background), image.Point{}, stddraw.Src)
	p, err := newPainter(canvas, renderScale)
	if err != nil {
		return nil, err
	}
	defer p.close()
	p.drawHeader(page)
	p.drawSummary(page)
	p.drawGroups(page)
	outputImage := image.NewRGBA(image.Rect(0, 0, imageWidth, height))
	xdraw.CatmullRom.Scale(outputImage, outputImage.Bounds(), canvas, canvas.Bounds(), xdraw.Src, nil)

	var output bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&output, outputImage); err != nil {
		return nil, fmt.Errorf("编码状态图: %w", err)
	}
	return output.Bytes(), nil
}

func newPainter(img *image.RGBA, scale int) (*painter, error) {
	collection, err := opentype.ParseCollection(notoSansCJK)
	if err != nil {
		return nil, errors.New("解析内嵌中文字体失败")
	}
	// Noto Sans CJK 集合顺序为 JP、KR、SC、TC、HK，索引 2 是简体中文。
	sc, err := collection.Font(2)
	if err != nil {
		return nil, errors.New("内嵌字体缺少简体中文字形")
	}
	p := &painter{img: img, faces: make(map[int]font.Face), scale: scale}
	for _, size := range []int{10, 11, 12, 13, 14, 15, 16, 18, 20, 22, 26, 30, 32} {
		face, faceErr := opentype.NewFace(sc, &opentype.FaceOptions{
			Size: float64(size * scale), DPI: 72, Hinting: font.HintingNone,
		})
		if faceErr != nil {
			p.close()
			return nil, errors.New("创建中文字体失败")
		}
		p.faces[size] = face
	}
	return p, nil
}

func (p *painter) close() {
	for _, face := range p.faces {
		_ = face.Close()
	}
}

func imageHeight(groups []statusapi.MonitorGroup) int {
	height := 270
	monitorCount := 0
	for _, group := range groups {
		columns := groupColumns(len(group.Monitors))
		rows := (len(group.Monitors) + columns - 1) / columns
		if rows == 0 {
			continue
		}
		monitorCount += len(group.Monitors)
		height += 43 + rows*cardHeight + (rows-1)*rowGap + 30
	}
	if monitorCount == 0 {
		return 500
	}
	return height
}

func groupColumns(monitors int) int {
	if monitors <= 2 {
		return 2
	}
	return 3
}

func (p *painter) drawHeader(page statusapi.StatusPage) {
	title := strings.TrimSpace(page.Title)
	if title == "" {
		title = "服务状态"
	}
	p.textFit(title+" 服务状态", margin, 72, 32, textMain, 760, true)
	description := strings.TrimSpace(page.Description)
	if description == "" {
		description = "上游服务实时运行概览"
	}
	p.textFit(description, margin, 104, 15, textMuted, 760, false)

	periodBox := image.Rect(994, 39, 1232, 79)
	p.borderRect(periodBox, 8, border, surface)
	p.text("统计周期  "+periodLabel(page.Period), 1014, 65, 14, textMain, true)
	p.textRight("数据更新时间  "+formatTimestamp(page.Timestamp), 1232, 104, 13, textMuted, false)
}

func (p *painter) drawSummary(page statusapi.StatusPage) {
	rect := image.Rect(margin, 126, imageWidth-margin, 234)
	p.borderRect(rect, 12, border, surface)
	total, online, abnormal := pageCounts(page)
	style := overallStyle(total, online)
	p.fillCircle(88, 180, 25, style.soft)
	p.fillCircle(88, 180, 14, style.color)
	if total > 0 && online == total {
		p.check(80, 180, style.soft)
	} else {
		p.textCentered("!", 88, 187, 18, style.soft, true)
	}
	p.text(style.header, 126, 174, 22, textMain, true)
	subtitle := fmt.Sprintf("共 %d 个节点，持续展示最近 100 次心跳", total)
	if total == 0 {
		subtitle = "状态页尚未返回监控节点"
	}
	p.text(subtitle, 126, 201, 14, textMuted, false)

	avg, avgOK := averageUptime(page)
	stats := []struct {
		label string
		value string
		color color.RGBA
	}{
		{label: "在线节点", value: fmt.Sprintf("%d / %d", online, total), color: statusFor(1).color},
		{label: "异常节点", value: strconv.Itoa(abnormal), color: statusFor(0).color},
		{label: "平均可用率", value: percent(avg, avgOK), color: textMain},
	}
	for i, stat := range stats {
		x := 748 + i*158
		if i > 0 {
			p.line(x-24, 154, x-24, 207, 1, border)
		}
		p.text(stat.label, x, 166, 13, textMuted, false)
		p.text(stat.value, x, 198, 22, stat.color, true)
	}
}

func (p *painter) drawGroups(page statusapi.StatusPage) {
	y := 270
	drawn := false
	for _, group := range page.Groups {
		if len(group.Monitors) == 0 {
			continue
		}
		drawn = true
		name := strings.TrimSpace(group.Name)
		if name == "" {
			name = "服务"
		}
		p.textFit(name, margin, y+26, 20, textMain, 800, true)
		p.textRight(fmt.Sprintf("%d 个节点", len(group.Monitors)), imageWidth-margin, y+24, 13, textMuted, false)
		y += 43
		columns := groupColumns(len(group.Monitors))
		cardWidth := (imageWidth - 2*margin - (columns-1)*cardGap) / columns
		for index, monitor := range group.Monitors {
			row, column := index/columns, index%columns
			x := margin + column*(cardWidth+cardGap)
			cardY := y + row*(cardHeight+rowGap)
			p.drawCard(image.Rect(x, cardY, x+cardWidth, cardY+cardHeight), page, monitor)
		}
		rows := (len(group.Monitors) + columns - 1) / columns
		y += rows*cardHeight + (rows-1)*rowGap + 30
	}
	if drawn {
		return
	}
	p.borderRect(image.Rect(margin, y, imageWidth-margin, y+174), 12, border, surface)
	p.textCentered("暂无可展示的监控数据", imageWidth/2, y+82, 18, textMain, true)
	p.textCentered("请检查状态图数据源、Page ID 和统计周期", imageWidth/2, y+112, 13, textMuted, false)
}

func (p *painter) drawCard(rect image.Rectangle, page statusapi.StatusPage, monitor statusapi.Monitor) {
	p.borderRect(rect, 10, border, surface)
	heartbeats := page.Heartbeats[monitor.ID]
	latestStatus := -1
	if len(heartbeats) > 0 {
		latestStatus = heartbeats[len(heartbeats)-1].Status
	}
	style := statusFor(latestStatus)
	p.fillCircle(rect.Min.X+24, rect.Min.Y+28, 6, style.color)
	p.textFit(monitor.Name, rect.Min.X+39, rect.Min.Y+35, 20, textMain, rect.Dx()-175, true)
	badgeWidth := p.measure(style.name, 13) + 24
	badge := image.Rect(rect.Max.X-badgeWidth-18, rect.Min.Y+16, rect.Max.X-18, rect.Min.Y+42)
	p.roundedRect(badge, 13, style.soft)
	p.textCentered(style.name, (badge.Min.X+badge.Max.X)/2, badge.Min.Y+19, 13, style.color, true)

	latest, average, latestOK, pingCount := pingStats(heartbeats)
	secondMetricX := rect.Min.X + 149
	valueSize := 26
	if rect.Dx() > 450 {
		secondMetricX = rect.Min.X + 210
		valueSize = 30
	}
	p.text("最新延迟", rect.Min.X+20, rect.Min.Y+78, 13, textMuted, false)
	p.text(formatPing(latest, latestOK), rect.Min.X+20, rect.Min.Y+112, valueSize, textMain, true)
	p.text("平均延迟", secondMetricX, rect.Min.Y+78, 13, textMuted, false)
	p.text(formatPing(average, pingCount > 0), secondMetricX, rect.Min.Y+110, 20, textMain, true)

	uptime, uptimeOK := page.Uptime[fmt.Sprintf("%d_selected", monitor.ID)]
	p.ring(rect.Max.X-54, rect.Min.Y+94, 31, uptime, uptimeOK, style.color)
	p.textCentered(percent(uptime, uptimeOK), rect.Max.X-54, rect.Min.Y+99, 13, textMain, true)
	p.textCentered(periodLabel(page.Period)+"可用率", rect.Max.X-54, rect.Min.Y+140, 12, textMuted, false)

	p.text("最近 100 次心跳", rect.Min.X+20, rect.Min.Y+173, 13, textMuted, true)
	p.statusLegend(rect.Max.X-20, rect.Min.Y+173)
	p.statusBar(rect.Min.X+20, rect.Min.Y+187, rect.Dx()-40, 11, heartbeats)

	p.line(rect.Min.X+20, rect.Min.Y+215, rect.Max.X-20, rect.Min.Y+215, 1, border)
	p.text("延迟趋势", rect.Min.X+20, rect.Min.Y+241, 13, textMuted, true)
	p.chart(image.Rect(rect.Min.X+20, rect.Min.Y+253, rect.Max.X-20, rect.Max.Y-18), heartbeats, style.color)
}

func (p *painter) statusLegend(right, baseline int) {
	items := []int{1, 2, 3, 0}
	totalWidth := 0
	for _, status := range items {
		totalWidth += 10 + p.measure(statusFor(status).name, 12) + 12
	}
	x := right - totalWidth + 12
	for _, status := range items {
		style := statusFor(status)
		p.fillCircle(x+3, baseline-5, 3, style.color)
		x += 10
		p.text(style.name, x, baseline, 12, style.color, true)
		x += p.measure(style.name, 12) + 12
	}
}

func (p *painter) statusBar(x, y, width, height int, heartbeats []statusapi.Heartbeat) {
	if len(heartbeats) > 100 {
		heartbeats = heartbeats[len(heartbeats)-100:]
	}
	if len(heartbeats) == 0 {
		p.roundedRect(image.Rect(x, y, x+width, y+height), 3, hex("#E8ECF1"))
		return
	}
	for i, heartbeat := range heartbeats {
		x0 := x + i*width/len(heartbeats)
		x1 := x + (i+1)*width/len(heartbeats) - 1
		if x1 <= x0 {
			x1 = x0 + 1
		}
		p.roundedRect(image.Rect(x0, y, x1, y+height), 1, statusFor(heartbeat.Status).color)
	}
}

func (p *painter) chart(rect image.Rectangle, heartbeats []statusapi.Heartbeat, lineColor color.RGBA) {
	if len(heartbeats) > 100 {
		heartbeats = heartbeats[len(heartbeats)-100:]
	}
	for i := 0; i < 3; i++ {
		y := rect.Min.Y + 12 + i*(rect.Dy()-32)/2
		p.dashedLine(rect.Min.X+46, y, rect.Max.X, y, chartGrid)
	}
	values := make([]float64, 0, len(heartbeats))
	for _, heartbeat := range heartbeats {
		if heartbeat.Ping != nil {
			values = append(values, *heartbeat.Ping)
		}
	}
	if len(values) == 0 {
		p.textCentered("暂无延迟数据", (rect.Min.X+rect.Max.X)/2, rect.Min.Y+58, 12, textMuted, false)
		return
	}
	maxPing := values[0]
	for _, value := range values[1:] {
		maxPing = math.Max(maxPing, value)
	}
	maxPing = math.Max(maxPing, 1)
	p.text(formatPing(maxPing, true), rect.Min.X, rect.Min.Y+16, 10, textMuted, false)
	p.text("0 ms", rect.Min.X, rect.Max.Y-16, 10, textMuted, false)
	chartLeft, chartRight := rect.Min.X+46, rect.Max.X
	chartTop, chartBottom := rect.Min.Y+12, rect.Max.Y-20
	points := make([]image.Point, 0, len(heartbeats))
	for i, heartbeat := range heartbeats {
		if heartbeat.Ping == nil {
			continue
		}
		x := chartLeft
		if len(heartbeats) > 1 {
			x += i * (chartRight - chartLeft) / (len(heartbeats) - 1)
		}
		ratio := math.Min(1, math.Max(0, *heartbeat.Ping/maxPing))
		y := chartBottom - int(ratio*float64(chartBottom-chartTop))
		points = append(points, image.Pt(x, y))
	}
	p.smoothCurve(points, image.Rect(chartLeft, chartTop, chartRight+1, chartBottom+1), lineColor)
	p.text(timeLabel(heartbeats, true), chartLeft, rect.Max.Y, 10, textMuted, false)
	p.textRight(timeLabel(heartbeats, false), chartRight, rect.Max.Y, 10, textMuted, false)
}

func pageCounts(page statusapi.StatusPage) (total, online, abnormal int) {
	for _, group := range page.Groups {
		for _, monitor := range group.Monitors {
			total++
			heartbeats := page.Heartbeats[monitor.ID]
			if len(heartbeats) > 0 && heartbeats[len(heartbeats)-1].Status == 1 {
				online++
			} else {
				abnormal++
			}
		}
	}
	return
}

func averageUptime(page statusapi.StatusPage) (float64, bool) {
	var total float64
	count := 0
	for _, group := range page.Groups {
		for _, monitor := range group.Monitors {
			if value, ok := page.Uptime[fmt.Sprintf("%d_selected", monitor.ID)]; ok {
				total += value
				count++
			}
		}
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func pingStats(heartbeats []statusapi.Heartbeat) (latest, average float64, latestOK bool, count int) {
	for _, heartbeat := range heartbeats {
		if heartbeat.Ping == nil {
			continue
		}
		average += *heartbeat.Ping
		count++
	}
	if len(heartbeats) > 0 && heartbeats[len(heartbeats)-1].Ping != nil {
		latest = *heartbeats[len(heartbeats)-1].Ping
		latestOK = true
	}
	if count > 0 {
		average /= float64(count)
	}
	return
}

func overallStyle(total, online int) statusStyle {
	if total == 0 {
		return statusFor(-1)
	}
	if total == online {
		style := statusFor(1)
		style.header = "所有服务运行正常"
		return style
	}
	style := statusFor(0)
	style.header = "部分服务需要关注"
	return style
}

func statusFor(status int) statusStyle {
	switch status {
	case 1:
		return statusStyle{name: "在线", color: hex("#13A967"), soft: hex("#E7F7EF"), header: "所有服务运行正常"}
	case 0:
		return statusStyle{name: "离线", color: hex("#D94F55"), soft: hex("#FCECEE"), header: "部分服务需要关注"}
	case 2:
		return statusStyle{name: "重试中", color: hex("#D98A16"), soft: hex("#FFF4DF")}
	case 3:
		return statusStyle{name: "维护中", color: hex("#3974C6"), soft: hex("#EAF1FB")}
	default:
		return statusStyle{name: "未知", color: hex("#7A8493"), soft: hex("#EEF1F4"), header: "暂无监控数据"}
	}
}

func percent(value float64, ok bool) string {
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.2f%%", value*100)
}

func formatPing(value float64, ok bool) string {
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	if value >= 1000 {
		return fmt.Sprintf("%.2f s", value/1000)
	}
	return fmt.Sprintf("%.0f ms", value)
}

func periodLabel(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "24h", "1d":
		return "近 24 小时"
	case "7d", "1w":
		return "近 7 天"
	case "30d", "1m":
		return "近 30 天"
	case "90d", "3m":
		return "近 90 天"
	case "1y":
		return "近 1 年"
	default:
		if strings.TrimSpace(period) == "" {
			return "选定周期"
		}
		return strings.TrimSpace(period)
	}
}

func formatTimestamp(timestamp int64) string {
	if timestamp <= 0 {
		return "--"
	}
	return time.UnixMilli(timestamp).Format("01-02 15:04")
}

func timeLabel(heartbeats []statusapi.Heartbeat, first bool) string {
	if len(heartbeats) == 0 {
		return ""
	}
	item := heartbeats[len(heartbeats)-1]
	if first {
		item = heartbeats[0]
	}
	parsed, err := time.Parse("2006-01-02 15:04:05 -0700", item.Time)
	if err != nil {
		return ""
	}
	return parsed.Local().Format("15:04")
}

func hex(value string) color.RGBA {
	value = strings.TrimPrefix(value, "#")
	number, _ := strconv.ParseUint(value, 16, 32)
	return color.RGBA{R: uint8(number >> 16), G: uint8(number >> 8), B: uint8(number), A: 255}
}

func (p *painter) text(value string, x, baseline, size int, c color.RGBA, bold bool) {
	drawer := font.Drawer{Dst: p.img, Src: image.NewUniform(c), Face: p.faces[size], Dot: fixed.P(x*p.scale, baseline*p.scale)}
	drawer.DrawString(value)
	if bold {
		drawer.Dot = fixed.P(x*p.scale+1, baseline*p.scale)
		drawer.DrawString(value)
	}
}

func (p *painter) textRight(value string, x, baseline, size int, c color.RGBA, bold bool) {
	p.text(value, x-p.measure(value, size), baseline, size, c, bold)
}

func (p *painter) textCentered(value string, x, baseline, size int, c color.RGBA, bold bool) {
	p.text(value, x-p.measure(value, size)/2, baseline, size, c, bold)
}

func (p *painter) textFit(value string, x, baseline, size int, c color.RGBA, maxWidth int, bold bool) {
	if p.measure(value, size) <= maxWidth {
		p.text(value, x, baseline, size, c, bold)
		return
	}
	runes := []rune(value)
	for len(runes) > 0 && p.measure(string(runes)+"…", size) > maxWidth {
		runes = runes[:len(runes)-1]
	}
	p.text(string(runes)+"…", x, baseline, size, c, bold)
}

func (p *painter) measure(value string, size int) int {
	return (font.MeasureString(p.faces[size], value).Ceil() + p.scale - 1) / p.scale
}

func (p *painter) roundedRect(rect image.Rectangle, radius int, c color.RGBA) {
	rect = image.Rect(rect.Min.X*p.scale, rect.Min.Y*p.scale, rect.Max.X*p.scale, rect.Max.Y*p.scale)
	radius *= p.scale
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			dx := max(rect.Min.X+radius-x, x-(rect.Max.X-radius-1), 0)
			dy := max(rect.Min.Y+radius-y, y-(rect.Max.Y-radius-1), 0)
			if dx*dx+dy*dy <= radius*radius {
				p.img.SetRGBA(x, y, c)
			}
		}
	}
}

func (p *painter) borderRect(rect image.Rectangle, radius int, stroke, fill color.RGBA) {
	p.roundedRect(rect, radius, stroke)
	p.roundedRect(rect.Inset(1), max(radius-1, 1), fill)
}

func (p *painter) fillCircle(cx, cy, radius int, c color.RGBA) {
	cx *= p.scale
	cy *= p.scale
	radius *= p.scale
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radius*radius {
				p.img.SetRGBA(x, y, c)
			}
		}
	}
}

func (p *painter) ring(cx, cy, radius int, value float64, ok bool, c color.RGBA) {
	cx *= p.scale
	cy *= p.scale
	radius *= p.scale
	progress := math.Max(0, math.Min(1, value))
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			distance := dx*dx + dy*dy
			if distance > radius*radius || distance < (radius-5)*(radius-5) {
				continue
			}
			pixel := hex("#E8ECF1")
			angle := math.Atan2(float64(dy), float64(dx)) + math.Pi/2
			if angle < 0 {
				angle += 2 * math.Pi
			}
			if ok && angle/(2*math.Pi) <= progress {
				pixel = c
			}
			p.img.SetRGBA(x, y, pixel)
		}
	}
}

func (p *painter) check(x, y int, c color.RGBA) {
	p.line(x, y, x+6, y+6, 3, c)
	p.line(x+6, y+6, x+17, y-7, 3, c)
}

func (p *painter) dashedLine(x1, y1, x2, y2 int, c color.RGBA) {
	for x := x1; x < x2; x += 8 {
		p.line(x, y1, min(x+4, x2), y2, 1, c)
	}
}

func (p *painter) smoothCurve(points []image.Point, bounds image.Rectangle, c color.RGBA) {
	if len(points) == 0 {
		return
	}
	if len(points) == 1 {
		p.fillCircle(points[0].X, points[0].Y, 2, c)
		return
	}
	previous := points[0]
	for index := 0; index < len(points)-1; index++ {
		p0 := points[max(index-1, 0)]
		p1 := points[index]
		p2 := points[index+1]
		p3 := points[min(index+2, len(points)-1)]
		for step := 1; step <= 6; step++ {
			t := float64(step) / 6
			x := catmullRom(float64(p0.X), float64(p1.X), float64(p2.X), float64(p3.X), t)
			y := catmullRom(float64(p0.Y), float64(p1.Y), float64(p2.Y), float64(p3.Y), t)
			point := image.Pt(
				min(max(int(math.Round(x)), bounds.Min.X), bounds.Max.X-1),
				min(max(int(math.Round(y)), bounds.Min.Y), bounds.Max.Y-1),
			)
			p.line(previous.X, previous.Y, point.X, point.Y, 2, c)
			previous = point
		}
	}
}

func catmullRom(p0, p1, p2, p3, t float64) float64 {
	t2 := t * t
	t3 := t2 * t
	return 0.5 * ((2 * p1) + (-p0+p2)*t + (2*p0-5*p1+4*p2-p3)*t2 + (-p0+3*p1-3*p2+p3)*t3)
}

func (p *painter) line(x0, y0, x1, y1, thickness int, c color.RGBA) {
	dx := abs(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -abs(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		p.fillCircle(x0, y0, max(thickness/2, 1), c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
