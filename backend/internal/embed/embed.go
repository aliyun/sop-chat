package embed

import (
	"embed"
	"io/fs"
)

//go:embed frontend/*
var frontendFS embed.FS

// GetFrontendFS 返回前端文件系统
func GetFrontendFS() fs.FS {
	// 从 embed.FS 中获取 frontend 子目录
	fsys, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		// 如果出错，返回空的文件系统
		return &emptyFS{}
	}
	return fsys
}

// emptyFS 是一个空的文件系统实现
type emptyFS struct{}

func (e *emptyFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}
