package statusimage

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/math/fixed"
	"qq-status-bot/internal/statusapi"
)

func TestRendererGolden(t *testing.T) {
	data, err := (Renderer{}).Render(fixturePage(2))
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "status.golden.png")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wantData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("读取 golden 文件: %v（使用 UPDATE_GOLDEN=1 更新）", err)
	}
	got := decodeRGBA(t, data)
	want := decodeRGBA(t, wantData)
	if got.Bounds() != image.Rect(0, 0, 1280, 708) {
		t.Fatalf("尺寸错误: %v", got.Bounds())
	}
	if got.Bounds() != want.Bounds() || !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("渲染结果与 golden PNG 不一致（使用 UPDATE_GOLDEN=1 更新）")
	}
}

func TestRendererStatusColorsChineseAndDynamicLayout(t *testing.T) {
	page := fixturePage(4)
	page.Heartbeats[1][99].Status = 0
	page.Heartbeats[2][99].Status = 2
	page.Heartbeats[3][99].Status = 3
	page.Heartbeats[4][99].Status = 99
	data, err := (Renderer{}).Render(page)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeRGBA(t, data)
	if img.Bounds() != image.Rect(0, 0, 1280, 1091) {
		t.Fatalf("多行卡片尺寸错误: %v", img.Bounds())
	}
	for _, status := range []int{0, 1, 2, 3, -1} {
		if countColor(img, statusFor(status).color) == 0 {
			t.Fatalf("状态 %d 的关键颜色未渲染", status)
		}
	}

	p, err := newPainter(image.NewRGBA(image.Rect(0, 0, 100, 100)), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	_, _, _, _, ok := p.faces[18].Glyph(fixed.P(0, 30), '服')
	if !ok {
		t.Fatal("内嵌字体缺少中文字形")
	}
}

func TestRendererHandlesEmptyData(t *testing.T) {
	data, err := (Renderer{}).Render(statusapi.StatusPage{Title: "空状态页", Period: "1y"})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeRGBA(t, data)
	if img.Bounds() != image.Rect(0, 0, 1280, 500) {
		t.Fatalf("空数据尺寸错误: %v", img.Bounds())
	}
}

func TestRendererRejectsExcessiveHeight(t *testing.T) {
	page := fixturePage(40)
	if _, err := (Renderer{}).Render(page); err == nil {
		t.Fatal("应拒绝超过最大高度的状态图")
	}
}

func fixturePage(monitors int) statusapi.StatusPage {
	page := statusapi.StatusPage{
		Title: "GGAPI", Description: "上游服务实时运行概览", Period: "1y", Timestamp: 1785421800000,
		Heartbeats: make(map[int][]statusapi.Heartbeat), Uptime: make(map[string]float64),
	}
	group := statusapi.MonitorGroup{ID: 1, Name: "服务"}
	for id := 1; id <= monitors; id++ {
		group.Monitors = append(group.Monitors, statusapi.Monitor{ID: id, Name: "节点 " + string(rune('A'+id-1)), Type: "http"})
		page.Uptime[string(rune('0'+id))+"_selected"] = 0.98 - float64(id)/100
		for index := 0; index < 100; index++ {
			ping := float64(80 + id*20 + index%17*8)
			status := 1
			if index%23 == 0 {
				status = 0
			}
			page.Heartbeats[id] = append(page.Heartbeats[id], statusapi.Heartbeat{
				Status: status, Time: "2026-07-30 12:00:00 +0000", Ping: &ping,
			})
		}
	}
	page.Groups = []statusapi.MonitorGroup{group}
	return page
}

func decodeRGBA(t *testing.T, data []byte) *image.RGBA {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	result := image.NewRGBA(decoded.Bounds())
	draw.Draw(result, result.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return result
}

func countColor(img *image.RGBA, target color.RGBA) int {
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if img.RGBAAt(x, y) == target {
				count++
			}
		}
	}
	return count
}
