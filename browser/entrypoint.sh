#!/bin/sh
set -eu

mkdir -p /tmp/chromium
chown -R app:nogroup /tmp/chromium

socat TCP-LISTEN:9222,fork,reuseaddr,bind=0.0.0.0 TCP:127.0.0.1:19222 &

exec runuser -u app -- chromium \
  --headless=new \
  --no-sandbox \
  --disable-dev-shm-usage \
  --disable-gpu \
  --disable-features=Translate,UseOzonePlatform,Vulkan \
  --hide-scrollbars \
  --user-data-dir=/tmp/chromium \
  --remote-debugging-address=127.0.0.1 \
  --remote-debugging-port=19222 \
  --remote-allow-origins=* \
  --no-first-run \
  --no-default-browser-check \
  about:blank
