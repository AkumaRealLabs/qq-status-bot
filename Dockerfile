FROM node:24-alpine AS frontend-builder

WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@10.33.4 --activate && pnpm install --frozen-lockfile
COPY frontend ./
RUN pnpm build

FROM golang:1.26-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -o /out/qq-status-bot .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates wget \
	&& adduser -D -H app
WORKDIR /app
COPY --from=builder /out/qq-status-bot /app/qq-status-bot
RUN mkdir -p /app/data && chown -R app:app /app
USER app
EXPOSE 8090
CMD ["/app/qq-status-bot"]
