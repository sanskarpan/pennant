package model

import "time"

type SDKKeyType string

const (
	SDKKeyServer SDKKeyType = "server"
	SDKKeyClient SDKKeyType = "client"
	SDKKeyMobile SDKKeyType = "mobile"
)

type SDKKey struct {
	Value     string     `json:"value"`
	Type      SDKKeyType `json:"type"`
	CreatedAt time.Time  `json:"created_at"`
}

type Environment struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Color      string   `json:"color"`
	ProjectKey string   `json:"project_key"`
	SDKKeys    []SDKKey `json:"sdk_keys"`
	Version    int64    `json:"version"`
}

type Project struct {
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Environments []Environment `json:"environments"`
}
