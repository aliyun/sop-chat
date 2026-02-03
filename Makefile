.PHONY: build build-frontend build-backend clean

# 构建前端和后端
build: build-frontend build-backend

# 构建前端
build-frontend:
	@echo "检查前端依赖..."
	@if [ ! -d "frontend/node_modules" ]; then \
		echo "未找到 node_modules，正在安装依赖..."; \
		cd frontend && npm install; \
	fi
	@echo "构建前端..."
	cd frontend && npm run build
	@echo "复制前端文件到 embed 目录..."
	@mkdir -p backend/internal/embed/frontend
	@rm -rf backend/internal/embed/frontend/*
	@cp -r frontend/dist/* backend/internal/embed/frontend/
	@echo "前端构建完成"

# 构建后端（需要先构建前端）
build-backend:
	@echo "构建后端..."
	cd backend && go build -o sop-chat-api ./cmd/sop-chat-api
	@echo "后端构建完成"

# 清理构建产物
clean:
	@echo "清理构建产物..."
	rm -rf frontend/dist
	rm -rf backend/internal/embed/frontend
	rm -f backend/sop-chat-api
	@echo "清理完成"
