#!/bin/bash
# GitLab Project Preparation Script
# Cleans up the project for initial GitLab push

set -e

echo "🧹 Cleaning up Hauler UI for GitLab..."

# Remove development artifacts
echo "Removing development artifacts..."
rm -rf node_modules/ 2>/dev/null || true
rm -rf .next/ 2>/dev/null || true
rm -rf dist/ 2>/dev/null || true
rm -rf build/ 2>/dev/null || true

# Clean test reports
echo "Cleaning test reports..."
rm -rf tests/reports/*.html 2>/dev/null || true
rm -rf tests/reports/*.xml 2>/dev/null || true

# Remove temporary files
echo "Removing temporary files..."
find . -name "*.log" -type f -delete 2>/dev/null || true
find . -name "*.tmp" -type f -delete 2>/dev/null || true
find . -name ".DS_Store" -type f -delete 2>/dev/null || true
find . -name "Thumbs.db" -type f -delete 2>/dev/null || true

# Clean data directory (keep structure)
echo "Cleaning data directory..."
rm -rf data/hauls/*.tar.zst 2>/dev/null || true
rm -rf data/manifests/*.yaml 2>/dev/null || true
rm -rf data/config/*.json 2>/dev/null || true

# Ensure required directories exist
echo "Ensuring directory structure..."
mkdir -p data/{store,manifests,hauls,config}
mkdir -p tests/reports
mkdir -p docs/wiki

# Create .gitkeep files for empty directories
touch data/store/.gitkeep
touch data/manifests/.gitkeep
touch data/hauls/.gitkeep
touch data/config/.gitkeep

# Update MCP server config path
echo "Updating MCP server configuration..."
if [ -f "mcp_server/mcp-config.json" ]; then
    sed -i 's|/home/user/Desktop/[^/]*/|./|g' mcp_server/mcp-config.json 2>/dev/null || true
fi

# Create GitLab-specific files
echo "Creating GitLab configuration files..."

# Verify required files exist
REQUIRED_FILES=(
    "README.md"
    "LICENSE"
    "Dockerfile"
    "docker-compose.yml"
    ".gitignore"
)

for file in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "$file" ]; then
        echo "⚠️  Warning: Required file missing: $file"
    fi
done

echo "✅ Cleanup complete!"
echo ""
echo "📋 Next steps:"
echo "1. Review and update .gitlab-ci.yml"
echo "2. Update README.md with your GitLab repository URL"
echo "3. Review WIKI_*.md files and upload to GitLab Wiki"
echo "4. Initialize git repository:"
echo "   git init"
echo "   git add ."
echo "   git commit -m 'Initial commit: Hauler UI v3.3.5'"
echo "   git remote add origin <your-gitlab-repo-url>"
echo "   git push -u origin main"
