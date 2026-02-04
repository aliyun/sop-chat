#!/bin/bash

set -e

echo "Starting to build sop-chat..."

# Check and install frontend dependencies
echo "Step 1/3: Checking frontend dependencies..."
cd frontend
if [ ! -d "node_modules" ]; then
    echo "node_modules not found, installing dependencies..."
    npm install
    echo "✓ Frontend dependencies installed"
else
    echo "✓ Frontend dependencies already exist"
fi

# Build frontend
echo "Step 2/3: Building frontend..."
npm run build
cd ..

# Copy frontend files to embed directory
echo "Step 3/3: Copying frontend files to embed directory..."
mkdir -p backend/internal/embed/frontend
rm -rf backend/internal/embed/frontend/*
cp -r frontend/dist/* backend/internal/embed/frontend/

# Build backend
echo "Step 3/3: Building backend..."
cd backend
go build -o sop-chat-server ./cmd/sop-chat-api
go build -o sop-chat-cli ./cmd/sop-chat-cli
cd ..

echo "Build complete! Binary files located at: backend/sop-chat-server"
