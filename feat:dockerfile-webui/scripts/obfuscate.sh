#!/bin/bash
set -e

echo "Installing javascript-obfuscator..."
npm install -g javascript-obfuscator

echo "Obfuscating app.js..."
javascript-obfuscator frontend/app.js \
  --output frontend/app.obfuscated.js \
  --compact true \
  --control-flow-flattening true \
  --control-flow-flattening-threshold 0.75 \
  --dead-code-injection true \
  --dead-code-injection-threshold 0.4 \
  --string-array true \
  --string-array-threshold 0.75 \
  --string-array-encoding 'base64' \
  --unicode-escape-sequence false

mv frontend/app.obfuscated.js frontend/app.js
echo "Obfuscation complete!"
