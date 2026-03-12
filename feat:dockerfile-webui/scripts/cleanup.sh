#!/bin/bash
set -e

echo "========================================="
echo "HAULER UI - REPOSITORY CLEANUP"
echo "========================================="
echo ""

cd /home/user/Desktop/hauler_ui

echo "🗑️  Removing redundant backend files..."
rm -f backend/main_original.go

echo "🗑️  Removing redundant frontend files..."
rm -f frontend/app_original.js
rm -f frontend/index_original.html

echo "🗑️  Removing binaries and archives..."
rm -f hauler
rm -f hauler-main.zip
rm -f QUICK_REFERENCE.txt

echo "🗑️  Removing Hauler source directory..."
rm -rf hauler-main/

echo "🗑️  Removing consolidated documentation..."
rm -f docs/PROJECT_SUMMARY.md
rm -f docs/PROJECT_COMPLETE.md
rm -f docs/EXECUTIVE_SUMMARY_V2.1.md
rm -f docs/FEATURE_IMPLEMENTATION_V2.1.md
rm -f docs/QUICK_START_V2.1.md
rm -f docs/PRODUCTION_READY_CORRECTED.md
rm -f docs/DOCUMENTATION_INDEX.md
rm -f docs/START_HERE.md

echo ""
echo "✅ Cleanup complete!"
echo ""
echo "📊 Repository Statistics:"
du -sh .
echo ""
echo "📁 Remaining Documentation:"
ls -lh docs/*.md 2>/dev/null | wc -l
echo ""
echo "📂 Agent Documentation:"
ls -1 docs/agents/*.md | wc -l
echo ""
echo "✨ Repository is now clean and organized!"
