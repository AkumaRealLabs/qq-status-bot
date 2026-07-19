FROM node:24-bookworm-slim AS source

ARG LLBOT_COMMIT=d6e2f485b8164597d04a2907d307739ecfcf4a55

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates git \
	&& rm -rf /var/lib/apt/lists/*

RUN git clone https://github.com/LLOneBot/LuckyLilliaBot.git /src \
	&& cd /src \
	&& git checkout --detach "$LLBOT_COMMIT" \
	&& test "$(git rev-parse HEAD)" = "$LLBOT_COMMIT" \
	&& rm -rf .git

FROM node:24-bookworm-slim AS builder

WORKDIR /src
COPY --from=source /src /src

RUN corepack enable && corepack prepare yarn@4.17.1 --activate
RUN yarn install --immutable \
	&& npm --prefix src/webui/FE install \
	&& yarn build-webui \
	&& yarn build

FROM node:24-alpine

RUN apk add --no-cache ffmpeg tzdata \
	&& ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
	&& echo "Asia/Shanghai" > /etc/timezone

ENV TZ=Asia/Shanghai
WORKDIR /app

COPY --from=builder /src/dist /app/dist
COPY default_config.json /app/dist/default_config.json
COPY entrypoint.sh /usr/local/bin/llbot-entrypoint

RUN chmod +x /usr/local/bin/llbot-entrypoint \
	&& for f in /app/dist/sign-proxy.*.node; do case "$f" in *-musl.node) ;; *) rm -f "$f" ;; esac; done

VOLUME ["/app/data"]
EXPOSE 3000 3080
ENTRYPOINT ["/usr/local/bin/llbot-entrypoint"]
