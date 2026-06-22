package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
	wxconfig "mr1v1-server/internal/wxserver/config"
)

type mapItem struct {
	Category string `json:"category"`
	MapName  string `json:"map_name"`
}

// ListMaps 代理 manager-backend 的地图池给小程序用于"指定地图"建房，
// 只返回已启用的地图，不暴露id/created_at等管理字段。
func ListMaps(cfg *wxconfig.WxConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := cfg.BackendURL + "/api/manager/maps"
		if category := c.Query("category"); category != "" {
			u += "?category=" + url.QueryEscape(category)
		}
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			resp.Fail(c, 500, "build request failed")
			return
		}
		if cfg.InternalAPIKey != "" {
			req.Header.Set("X-API-Key", cfg.InternalAPIKey)
		}
		client := &http.Client{Timeout: 10 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			resp.Fail(c, 502, "backend unreachable")
			return
		}
		defer res.Body.Close()

		var full struct {
			Code int `json:"code"`
			Data []struct {
				Category string `json:"category"`
				MapName  string `json:"map_name"`
				Enabled  bool   `json:"enabled"`
			} `json:"data"`
		}
		if err := json.NewDecoder(res.Body).Decode(&full); err != nil {
			resp.Fail(c, 502, "backend bad response")
			return
		}

		result := make([]mapItem, 0, len(full.Data))
		for _, m := range full.Data {
			if !m.Enabled {
				continue
			}
			result = append(result, mapItem{Category: m.Category, MapName: m.MapName})
		}
		resp.OK(c, result)
	}
}
