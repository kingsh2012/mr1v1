// Package version 保存agent二进制的构建版本号，由CI在go build时通过-ldflags -X注入。
// 本地go run/go build不传ldflags时为空字符串，backend侧版本校验会跳过空版本的agent
// （兼容本地调试，不强制要求开发环境也设置版本号）。
package version

var (
	Version   = ""
	BuildTime = ""
	GitCommit = ""
)
