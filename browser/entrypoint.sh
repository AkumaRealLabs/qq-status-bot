#!/bin/sh
set -eu

mkdir -p /profile
# ponytail: repair the named Docker volume in-place; no init sidecar for one chmod-class bug.
chown -R app:nogroup /profile
rm -f /tmp/.X99-lock /profile/SingletonLock /profile/SingletonSocket /profile/SingletonCookie /profile/DevToolsActivePort

exec runuser -u app -- sh -eu -c '
  Xvfb :99 -screen 0 1440x900x24 -nolisten tcp &
  for i in $(seq 1 50); do xdpyinfo -display :99 >/dev/null 2>&1 && break; sleep 0.1; done
  x11vnc -display :99 -forever -shared -nopw -rfbport 5900 &
  websockify --web=/usr/share/novnc 6080 localhost:5900 &
  socat TCP-LISTEN:9222,fork,reuseaddr,bind=0.0.0.0 TCP:127.0.0.1:19222 &
  DISPLAY=:99 exec chromium \
    --no-sandbox \
    --disable-dev-shm-usage \
    --disable-gpu \
    --disable-features=UseOzonePlatform,Vulkan \
    --ozone-platform=x11 \
    --user-data-dir=/profile \
    --remote-debugging-address=127.0.0.1 \
    --remote-debugging-port=19222 \
    --remote-allow-origins=* \
    --no-first-run \
    --no-default-browser-check \
    about:blank
'
