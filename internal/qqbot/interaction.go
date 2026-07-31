package qqbot

const (
	EventInteractionCreate = "INTERACTION_CREATE"

	InteractionTypeMessageButton = 11
	InteractionTypeQuickMenu     = 12

	ButtonActionCallback = 1
	ButtonPermissionUser = 0
	ButtonPermissionAll  = 2
)

// Interaction 是 QQ 推送的按钮或快捷菜单互动事件。
type Interaction struct {
	ID                string          `json:"id"`
	Type              int             `json:"type"`
	Scene             string          `json:"scene"`
	ChatType          int             `json:"chat_type"`
	GroupOpenID       string          `json:"group_openid"`
	GroupMemberOpenID string          `json:"group_member_openid"`
	Data              InteractionData `json:"data"`
}

type InteractionData struct {
	Type     int                 `json:"type"`
	Resolved InteractionResolved `json:"resolved"`
}

type InteractionResolved struct {
	ButtonData string `json:"button_data"`
	ButtonID   string `json:"button_id"`
}

type Keyboard struct {
	ID      string           `json:"id,omitempty"`
	Content *KeyboardContent `json:"content,omitempty"`
}

type KeyboardContent struct {
	Rows []KeyboardRow `json:"rows"`
}

type KeyboardRow struct {
	Buttons []KeyboardButton `json:"buttons"`
}

type KeyboardButton struct {
	ID         string               `json:"id"`
	RenderData ButtonRenderData     `json:"render_data"`
	Action     KeyboardButtonAction `json:"action"`
}

type ButtonRenderData struct {
	Label        string `json:"label"`
	VisitedLabel string `json:"visited_label,omitempty"`
	Style        int    `json:"style"`
}

type KeyboardButtonAction struct {
	Type          int              `json:"type"`
	Permission    ButtonPermission `json:"permission"`
	Data          string           `json:"data"`
	UnsupportTips string           `json:"unsupport_tips,omitempty"`
}

type ButtonPermission struct {
	Type           int      `json:"type"`
	SpecifyUserIDs []string `json:"specify_user_ids,omitempty"`
}

func (k Keyboard) Empty() bool {
	return k.ID == "" && (k.Content == nil || len(k.Content.Rows) == 0)
}
