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
    socat \
    && chromium --version | grep -q "Chromium 149.0.7827.196" \
    && rm -rf /var/lib/apt/lists/*

RUN adduser --system --uid 10001 --ingroup nogroup --home /tmp/chromium --no-create-home app

COPY entrypoint.sh /usr/local/bin/browser-entrypoint
RUN chmod +x /usr/local/bin/browser-entrypoint

EXPOSE 9222
CMD ["browser-entrypoint"]
