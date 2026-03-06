package embed

import (
	"embed"
	"io"
	"io/fs"
)

//go:embed frontend/*
var frontendFS embed.FS

// GetFrontendFS 返回前端文件系统
func GetFrontendFS() fs.FS {
	fsys, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return &emptyFS{}
	}
	return fsys
}

// GetConfigHTML 返回配置管理 UI 页面的 HTML 字节，若文件不存在则返回 nil。
// 源文件位于 frontend/public/config.html，通过 make build-frontend 构建后复制至此。
func GetConfigHTML() []byte {
	fsys := GetFrontendFS()
	f, err := fsys.Open("config.html")
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return data
}

// emptyFS 是一个空的文件系统实现
type emptyFS struct{}

func (e *emptyFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}
