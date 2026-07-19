FROM node:24-alpine AS frontend-builder

ARG VITE_BUILD_VERSION=dev
ENV VITE_BUILD_VERSION=$VITE_BUILD_VERSION
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@10.33.4 --activate && pnpm install --frozen-lockfile
COPY frontend ./
RUN pnpm build

FROM alpine:3.22 AS font-builder

ARG LXGW_WENKAI_VERSION=v1.522
ARG MAPLE_MONO_VERSION=v7.9
RUN apk add --no-cache ca-certificates curl unzip \
	&& mkdir -p /out/fonts \
	&& curl -fsSL --retry 3 -o /tmp/LXGWWenKai-Regular.ttf "https://github.com/lxgw/LxgwWenKai/releases/download/${LXGW_WENKAI_VERSION}/LXGWWenKai-Regular.ttf" \
	&& echo "39ad71264b588165b469e35e6afb162a378dacd1f95348160240ba9038ac3009  /tmp/LXGWWenKai-Regular.ttf" | sha256sum -c - \
	&& curl -fsSL --retry 3 -o /tmp/MapleMono-TTF.zip "https://github.com/subframe7536/maple-font/releases/download/${MAPLE_MONO_VERSION}/MapleMono-TTF.zip" \
	&& echo "3a35f8f0669bef3dded9df208cc4526a6f7573210e134816e9084a8981271d75  /tmp/MapleMono-TTF.zip" | sha256sum -c - \
	&& mv /tmp/LXGWWenKai-Regular.ttf /out/fonts/LXGWWenKai-Regular.ttf \
	&& unzip -j /tmp/MapleMono-TTF.zip MapleMono-Regular.ttf -d /out/fonts \
	&& test -s /out/fonts/LXGWWenKai-Regular.ttf \
	&& test -s /out/fonts/MapleMono-Regular.ttf

FROM golang:1.26-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -o /out/ai-upstream-monitor .

FROM alpine:3.22

ARG CODEX_CLI_VERSION=0.142.5
RUN apk add --no-cache nodejs npm wget \
	&& npm install -g @openai/codex@${CODEX_CLI_VERSION} \
	&& npm cache clean --force \
	&& adduser -D -H app
WORKDIR /app
COPY --from=builder /out/ai-upstream-monitor /app/ai-upstream-monitor
COPY --from=font-builder /out/fonts /app/fonts
RUN mkdir -p /app/data /app/pb_data && chown -R app:app /app
USER app
EXPOSE 8090
CMD ["/app/ai-upstream-monitor"]
