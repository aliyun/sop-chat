.PHONY: build build-frontend build-backend build-cli build-linux build-all clean clean-dist

# 颜色定义
GREEN := \033[0;32m
BLUE := \033[0;34m
YELLOW := \033[1;33m
NC := \033[0m

DIST_DIR := dist

# 默认目标：构建当前平台
build: build-frontend build-backend build-cli
	@echo "$(GREEN)构建完成！二进制文件位于: backend/$(NC)"

# 构建前端
build-frontend:
	@echo "$(BLUE)检查前端依赖...$(NC)"
	@if [ ! -d "frontend/node_modules" ]; then \
		echo "$(YELLOW)未找到 node_modules，正在安装依赖...$(NC)"; \
		cd frontend && npm install; \
		echo "$(GREEN)✓ 前端依赖安装完成$(NC)"; \
	else \
		echo "$(GREEN)✓ 前端依赖已存在$(NC)"; \
	fi
	@echo "$(BLUE)构建前端...$(NC)"
	cd frontend && npm run build
	@echo "$(BLUE)复制前端文件到 embed 目录...$(NC)"
	@mkdir -p backend/internal/embed/frontend
	@rm -rf backend/internal/embed/frontend/*
	@cp -r frontend/dist/* backend/internal/embed/frontend/
	@echo "$(GREEN)✓ 前端构建完成$(NC)"

# 构建后端（当前平台）
build-backend:
	@echo "$(BLUE)构建后端...$(NC)"
	cd backend && go build -o sop-chat-server ./cmd/sop-chat-server
	@echo "$(GREEN)✓ 后端构建完成$(NC)"

# 构建 CLI 工具（当前平台）
build-cli:
	@echo "$(BLUE)构建 CLI 工具...$(NC)"
	cd backend && go build -o sop-chat-cli ./cmd/sop-chat-cli
	@echo "$(GREEN)✓ CLI 构建完成$(NC)"

# 构建 Linux 版本（amd64）
build-linux: build-frontend
	@echo "$(GREEN)=========================================="
	@echo "构建 Linux 版本 (amd64)"
	@echo "==========================================$(NC)"
	@mkdir -p $(DIST_DIR)/linux
	@echo "$(BLUE)构建 Linux 服务端...$(NC)"
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "../$(DIST_DIR)/linux/sop-chat-server" ./cmd/sop-chat-server
	@echo "$(GREEN)✓ Linux 服务端构建完成: $(DIST_DIR)/linux/sop-chat-server$(NC)"
	@ls -lh "$(DIST_DIR)/linux/sop-chat-server" | awk '{print "  文件大小: " $$5}'
	@echo "$(BLUE)构建 Linux CLI...$(NC)"
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "../$(DIST_DIR)/linux/sop-chat-cli" ./cmd/sop-chat-cli
	@echo "$(GREEN)✓ Linux CLI 构建完成: $(DIST_DIR)/linux/sop-chat-cli$(NC)"
	@ls -lh "$(DIST_DIR)/linux/sop-chat-cli" | awk '{print "  文件大小: " $$5}'

# 多平台构建（Linux + macOS）
build-all: build-frontend
	@echo "$(GREEN)=========================================="
	@echo "构建 sop-chat (Linux 和 macOS)"
	@echo "==========================================$(NC)"
	
	@mkdir -p $(DIST_DIR)/linux
	@mkdir -p $(DIST_DIR)/darwin
	
	@echo "$(BLUE)构建 Linux 版本 (amd64)...$(NC)"
	cd backend && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "../$(DIST_DIR)/linux/sop-chat-server" ./cmd/sop-chat-server
	@echo "$(GREEN)✓ Linux 版本构建完成: $(DIST_DIR)/linux/sop-chat-server$(NC)"
	@ls -lh "$(DIST_DIR)/linux/sop-chat-server" | awk '{print "  文件大小: " $$5}'
	
	@echo "$(BLUE)构建 macOS 版本 (amd64)...$(NC)"
	cd backend && GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o "../$(DIST_DIR)/darwin/sop-chat-server" ./cmd/sop-chat-server
	@echo "$(GREEN)✓ macOS 版本构建完成: $(DIST_DIR)/darwin/sop-chat-server$(NC)"
	@ls -lh "$(DIST_DIR)/darwin/sop-chat-server" | awk '{print "  文件大小: " $$5}'
	
	@echo "$(BLUE)构建 macOS ARM64 版本 (Apple Silicon)...$(NC)"
	cd backend && GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o "../$(DIST_DIR)/darwin/sop-chat-server-arm64" ./cmd/sop-chat-server
	@echo "$(GREEN)✓ macOS ARM64 版本构建完成: $(DIST_DIR)/darwin/sop-chat-server-arm64$(NC)"
	@ls -lh "$(DIST_DIR)/darwin/sop-chat-server-arm64" | awk '{print "  文件大小: " $$5}'
	
	@echo ""
	@echo "$(GREEN)=========================================="
	@echo "构建完成！"
	@echo "==========================================$(NC)"
	@echo ""
	@echo "构建产物:"
	@echo "  - Linux (amd64):    $(DIST_DIR)/linux/sop-chat-server"
	@echo "  - macOS (amd64):    $(DIST_DIR)/darwin/sop-chat-server"
	@echo "  - macOS (arm64):    $(DIST_DIR)/darwin/sop-chat-server-arm64"
	@echo ""
	@echo "使用方式:"
	@echo "  Linux:       ./$(DIST_DIR)/linux/sop-chat-server"
	@echo "  macOS:       ./$(DIST_DIR)/darwin/sop-chat-server"
	@echo "  macOS M1/M2: ./$(DIST_DIR)/darwin/sop-chat-server-arm64"
	@echo ""

# 清理构建产物
clean: clean-dist
	@echo "$(BLUE)清理构建产物...$(NC)"
	rm -rf frontend/dist
	rm -rf backend/internal/embed/frontend
	rm -f backend/sop-chat-server
	rm -f backend/sop-chat-cli
	@echo "$(GREEN)✓ 清理完成$(NC)"

# 仅清理多平台构建产物
clean-dist:
	@echo "$(BLUE)清理多平台构建产物...$(NC)"
	rm -rf $(DIST_DIR)
	@echo "$(GREEN)✓ 多平台构建产物已清理$(NC)"
