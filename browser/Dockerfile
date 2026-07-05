FROM debian:bookworm-slim@sha256:1def178129dfb5f24db43afbf2fcac04530012e3264ba4ff81c71184e17a9ee4

ENV LANG=C.UTF-8 LC_ALL=C.UTF-8
ARG DEBIAN_SNAPSHOT=20260704T000000Z
ARG CHROMIUM_VERSION=149.0.7827.196-1~deb12u1

RUN rm -f /etc/apt/sources.list.d/* && \
    printf '%s\n' \
      "deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ bookworm main" \
      "deb [check-valid-until=no] http://snapshot.debian.org/archive/debian-security/${DEBIAN_SNAPSHOT}/ bookworm-security main" \
      > /etc/apt/sources.list && \
    apt-get -o Acquire::Check-Valid-Until=false update && apt-get install -y --no-install-recommends \
    ca-certificates \
    chromium=${CHROMIUM_VERSION} \
    fonts-wqy-zenhei \
    novnc \
    socat \
    websockify \
    x11vnc \
    x11-utils \
    xvfb \
    && chromium --version | grep -q "Chromium 149.0.7827.196" \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /profile

EXPOSE 6080 9222
CMD rm -f /tmp/.X99-lock /profile/SingletonLock /profile/SingletonSocket /profile/SingletonCookie /profile/DevToolsActivePort && \
    Xvfb :99 -screen 0 1440x900x24 -nolisten tcp & \
    for i in $(seq 1 50); do xdpyinfo -display :99 >/dev/null 2>&1 && break; sleep 0.1; done; \
    x11vnc -display :99 -forever -shared -nopw -rfbport 5900 & \
    websockify --web=/usr/share/novnc 6080 localhost:5900 & \
    socat TCP-LISTEN:9222,fork,reuseaddr,bind=0.0.0.0 TCP:127.0.0.1:19222 & \
    DISPLAY=:99 chromium \
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
