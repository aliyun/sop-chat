#!/bin/bash

set -e

echo "=========================================="
echo "Building sop-chat (Linux and macOS)"
echo "=========================================="

# Color definitions
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check and install frontend dependencies
echo -e "${BLUE}Step 1/5: Checking frontend dependencies...${NC}"
cd frontend
if [ ! -d "node_modules" ]; then
    echo -e "${YELLOW}node_modules not found, installing dependencies...${NC}"
    if ! npm install; then
        echo -e "${YELLOW}Error: Frontend dependency installation failed${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Frontend dependencies installed${NC}"
else
    echo -e "${GREEN}✓ Frontend dependencies already exist${NC}"
fi

# Build frontend
echo -e "${BLUE}Step 2/5: Building frontend...${NC}"
if ! npm run build; then
    echo -e "${YELLOW}Error: Frontend build failed${NC}"
    exit 1
fi
cd ..

# Copy frontend files to embed directory
echo -e "${BLUE}Step 3/5: Copying frontend files to embed directory...${NC}"
mkdir -p backend/internal/embed/frontend
rm -rf backend/internal/embed/frontend/*
cp -r frontend/dist/* backend/internal/embed/frontend/
echo -e "${GREEN}✓ Frontend files copied${NC}"

# Create output directory
DIST_DIR="dist"
mkdir -p "${DIST_DIR}/linux"
mkdir -p "${DIST_DIR}/darwin"

# Build Linux version
echo -e "${BLUE}Step 4/5: Building Linux version (amd64)...${NC}"
cd backend
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "../${DIST_DIR}/linux/sop-chat-api" ./cmd/sop-chat-api
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Linux version built successfully: ${DIST_DIR}/linux/sop-chat-api${NC}"
    # Display file size
    ls -lh "../${DIST_DIR}/linux/sop-chat-api" | awk '{print "  File size: " $5}'
else
    echo -e "${YELLOW}✗ Linux version build failed${NC}"
    exit 1
fi

# Build macOS version
echo -e "${BLUE}Step 5/5: Building macOS version (amd64)...${NC}"
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o "../${DIST_DIR}/darwin/sop-chat-api" ./cmd/sop-chat-api
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ macOS version built successfully: ${DIST_DIR}/darwin/sop-chat-api${NC}"
    # Display file size
    ls -lh "../${DIST_DIR}/darwin/sop-chat-api" | awk '{print "  File size: " $5}'
else
    echo -e "${YELLOW}✗ macOS version build failed${NC}"
    exit 1
fi

# Build macOS ARM64 version (Apple Silicon)
echo -e "${BLUE}Extra: Building macOS ARM64 version (arm64)...${NC}"
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o "../${DIST_DIR}/darwin/sop-chat-api-arm64" ./cmd/sop-chat-api
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ macOS ARM64 version built successfully: ${DIST_DIR}/darwin/sop-chat-api-arm64${NC}"
    # Display file size
    ls -lh "../${DIST_DIR}/darwin/sop-chat-api-arm64" | awk '{print "  File size: " $5}'
fi

cd ..

echo ""
echo -e "${GREEN}=========================================="
echo "Build complete!"
echo "==========================================${NC}"
echo ""
echo "Build artifacts:"
echo "  - Linux (amd64):   ${DIST_DIR}/linux/sop-chat-api"
echo "  - macOS (amd64):   ${DIST_DIR}/darwin/sop-chat-api"
echo "  - macOS (arm64):   ${DIST_DIR}/darwin/sop-chat-api-arm64"
echo ""
echo "Usage:"
echo "  Linux:       ./${DIST_DIR}/linux/sop-chat-api"
echo "  macOS:       ./${DIST_DIR}/darwin/sop-chat-api"
echo "  macOS M1/M2: ./${DIST_DIR}/darwin/sop-chat-api-arm64"
echo ""
