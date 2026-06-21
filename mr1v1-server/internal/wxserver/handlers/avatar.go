package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"mr1v1-server/internal/resp"
)

const maxAvatarBytes = 2 << 20 // 2MB

var allowedAvatarExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// UploadAvatar 接收小程序chooseAvatar组件选完头像后上传的图片文件，存到本机磁盘并
// 返回一个外部可访问的永久URL。chooseAvatar回调里给的avatarUrl只是本机临时文件路径，
// 别的玩家/房间列表根本看不到，必须实际把文件传上来才有意义。
func UploadAvatar(avatarsDir, publicURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := c.FormFile("avatar")
		if err != nil {
			resp.Fail(c, 400, "avatar file required")
			return
		}
		if file.Size <= 0 || file.Size > maxAvatarBytes {
			resp.Fail(c, 400, "avatar too large (max 2MB)")
			return
		}

		ext := strings.ToLower(filepath.Ext(file.Filename))
		if !allowedAvatarExt[ext] {
			ext = ".jpg"
		}

		oid := unsafeFilenameChars.ReplaceAllString(openid(c), "_")
		filename := fmt.Sprintf("%s-%d%s", oid, time.Now().UnixNano(), ext)

		if err := os.MkdirAll(avatarsDir, 0o755); err != nil {
			resp.Fail(c, 500, "save failed")
			return
		}
		dst := filepath.Join(avatarsDir, filename)
		if err := c.SaveUploadedFile(file, dst); err != nil {
			resp.Fail(c, 500, "save failed")
			return
		}

		url := strings.TrimRight(publicURL, "/") + "/api/wx/static/avatars/" + filename
		resp.OK(c, gin.H{"url": url})
	}
}
