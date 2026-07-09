package app

// ExportData is the backup/restore payload returned by ExportData / accepted by ImportData.
// JSON shape matches the store-layer format (version + tables); httpapi must not import store for this DTO.
type ExportData struct {
	Version string              `json:"version"`
	Tables  map[string][]RowMap `json:"tables"`
}

// RowMap is one exported table row (column → value).
type RowMap map[string]any
