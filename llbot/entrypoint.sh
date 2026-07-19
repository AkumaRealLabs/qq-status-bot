#!/bin/sh
set -eu

mkdir -p /app/data
exec node --enable-source-maps /app/dist/llbot.js
