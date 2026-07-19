package statusimage

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"ai-upstream-monitor/internal/domain"
)

func TestRendererCreatesPNGPagesForIndividualPublicCards(t *testing.T) {
	renderer, err := NewFromFontData(goregular.TTF, goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	renderer.Now = func() time.Time { return time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC) }
	rows := make([]domain.PublicModelCard, 0, pageCardLimit+1)
	for i := 0; i < pageCardLimit+1; i++ {
		rows = append(rows, domain.PublicModelCard{
			Name:         "NEWAPI-" + string(rune('A'+i)),
			DisplayGroup: "NEWAPI",
			History: []domain.PublicProbeRun{{
				Status: "正常", LatencyMS: 100 + i,
				// 图片渲染器必须忽略这些可能敏感的公开 DTO 字段。
				Input: "probe-input", Output: "probe-output", Error: "raw-error",
			}},
		})
	}
	pages, err := renderer.Render(domain.PublicMonitorStatus{Window: "1h", Rows: rows, Requests: 11, SuccessRate: 100, AvgLatency: 105})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(pages))
	}
	for i, page := range pages {
		decoded, err := png.Decode(bytes.NewReader(page))
		if err != nil {
			t.Fatalf("page %d is not PNG: %v", i, err)
		}
		if decoded.Bounds().Dx() != canvasWidth || decoded.Bounds().Dy() <= headerHeight+metricHeight {
			t.Fatalf("page %d bounds = %v", i, decoded.Bounds())
		}
	}
}

func TestPublicRenderCardsUsesOnlyDisplayFields(t *testing.T) {
	cards, groups := publicRenderCards([]domain.PublicModelCard{{
		Name: "公开卡片", DisplayGroup: "公开分组",
		History: []domain.PublicProbeRun{{Status: "请求失败", LatencyMS: 91, Input: "secret-input", Output: "secret-output", Error: "raw-error"}},
	}})
	if len(cards) != 1 || cards[0].name != "公开卡片" || cards[0].group != "公开分组" {
		t.Fatalf("cards = %+v", cards)
	}
	if cards[0].status.label != "异常" || cards[0].latencyMS != 91 || !cards[0].hasLatency {
		t.Fatalf("card status = %+v", cards[0])
	}
	if got := groups["公开分组"]; got.abnormal != 1 || got.total != 1 {
		t.Fatalf("group = %+v", got)
	}
}

func TestBuildPagesGroupsInterleavedRowsInPublicPageOrder(t *testing.T) {
	pages := buildPages(domain.PublicMonitorStatus{Rows: []domain.PublicModelCard{
		{Name: "A-1", DisplayGroup: "A", History: []domain.PublicProbeRun{{Status: "正常"}}},
		{Name: "B-1", DisplayGroup: "B", History: []domain.PublicProbeRun{{Status: "正常"}}},
		{Name: "A-2", DisplayGroup: "A", History: []domain.PublicProbeRun{{Status: "正常"}}},
	}})
	if len(pages) != 1 || len(pages[0].groups) != 2 {
		t.Fatalf("pages = %+v", pages)
	}
	if group := pages[0].groups[0]; group.summary.name != "A" || len(group.cards) != 2 || group.cards[1].name != "A-2" {
		t.Fatalf("first group = %+v", group)
	}
	if group := pages[0].groups[1]; group.summary.name != "B" || len(group.cards) != 1 {
		t.Fatalf("second group = %+v", group)
	}
}
