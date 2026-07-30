package domain

import "strings"

type GroupMessage struct {
	ID          string `json:"id"`
	GroupOpenID string `json:"group_openid"`
	Content     string `json:"content"`
	Author      struct {
		Bot bool `json:"bot"`
	} `json:"author"`
}

func IsCommand(content string, commands []string) bool {
	content = strings.TrimSpace(content)
	for _, command := range commands {
		if strings.EqualFold(content, strings.TrimSpace(command)) {
			return true
		}
	}
	return false
}
