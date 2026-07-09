package app

// ExportData：ExportData 返回 / ImportData 接受的备份恢复载荷。
// JSON 形状与 store 层一致（version + tables）；httpapi 不应为该 DTO import store。
type ExportData struct {
	Version string              `json:"version"`
	Tables  map[string][]RowMap `json:"tables"`
}

// RowMap 是导出表的一行（列 → 值）。
type RowMap map[string]any
