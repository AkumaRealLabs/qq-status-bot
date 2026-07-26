package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"ai-upstream-monitor/internal/domain"
)

// GGAPI 各版本 /groups 返回过 map、数组、纯字符串数组几种形状，都要能解析。
func TestSchedulerGroupsAcceptsUpstreamShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want []domain.SchedulerGroup
	}{
		{
			name: "map 形状：键是组名，值是倍率",
			raw:  `{"data":{"vip":1.5,"default":1}}`,
			want: []domain.SchedulerGroup{{Name: "default", Ratio: "1"}, {Name: "vip", Ratio: "1.5"}},
		},
		{
			name: "map 形状：值是对象",
			raw:  `{"data":{"vip":{"rate_multiplier":2,"desc":"高价"}}}`,
			want: []domain.SchedulerGroup{{Name: "vip", Ratio: "2", Description: "高价"}},
		},
		{
			name: "数组形状：对象带 group_name/group_ratio",
			raw:  `{"groups":[{"group_name":"low","group_ratio":0.5,"remark":"便宜"}]}`,
			want: []domain.SchedulerGroup{{Name: "low", Ratio: "0.5", Description: "便宜"}},
		},
		{
			name: "数组形状：纯字符串",
			raw:  `{"data":["alpha","beta"]}`,
			want: []domain.SchedulerGroup{{Name: "alpha"}, {Name: "beta"}},
		},
		{
			name: "items 嵌套数组",
			raw:  `{"data":{"items":[{"name":"nested"}]}}`,
			want: []domain.SchedulerGroup{{Name: "nested"}},
		},
		{
			name: "空 data 返回空列表而不是 panic",
			raw:  `{"data":null}`,
			want: []domain.SchedulerGroup{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &raw); err != nil {
				t.Fatal(err)
			}
			got := schedulerGroups(raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// 倍率必须保持原样文本，不能被 float 格式化成 1e+06 之类。
func TestSchedulerRatioStringKeepsPlainDecimal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"nil 返回空", nil, ""},
		{"整数 float 不带小数点", float64(1), "1"},
		{"小数原样", float64(0.375), "0.375"},
		{"大数不用科学计数法", float64(1000000), "1000000"},
		{"json.Number 原样", json.Number("2.50"), "2.50"},
		{"字符串去空格", "  1.25  ", "1.25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := schedulerRatioString(tc.value); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSchedulerStringsAcceptsUpstreamShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{"any 数组过滤空串", []any{"a", "", "  ", "b"}, []string{"a", "b"}},
		{"string 数组直通", []string{"a", "b"}, []string{"a", "b"}},
		{"JSON 字符串反序列化", `["a","b"]`, []string{"a", "b"}},
		{"非法 JSON 字符串返回空", `not-json`, nil},
		{"数字返回空", float64(1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := schedulerStrings(tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// 上游失败响应的错误文案要能取到，取不到也不能是空字符串。
func TestSchedulerMessagePicksFirstNonEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  map[string]any
		want string
	}{
		{"message 优先", map[string]any{"message": "配额不足", "error": "quota"}, "配额不足"},
		{"回落 error", map[string]any{"error": "quota"}, "quota"},
		{"回落 msg", map[string]any{"msg": "bad"}, "bad"},
		{"跳过 nil 值", map[string]any{"message": nil, "error": "real"}, "real"},
		{"全空给兜底文案", map[string]any{}, "调度器返回失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := schedulerMessage(tc.raw); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSchedulerArrayUnwrapsNestedKeys(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  int
	}{
		{"直接数组", []any{1, 2}, 2},
		{"items 键", map[string]any{"items": []any{1}}, 1},
		{"channels 键", map[string]any{"channels": []any{1, 2, 3}}, 3},
		{"data 键", map[string]any{"data": []any{1}}, 1},
		{"无匹配键", map[string]any{"other": []any{1}}, 0},
		{"标量", "x", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(schedulerArray(tc.value)); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
