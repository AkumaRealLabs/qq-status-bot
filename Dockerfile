FROM node:24-alpine AS frontend-builder

ENV CI=true
WORKDIR /src/frontend
COPY frontend ./
RUN npm install -g pnpm@10.33.4 \
    && pnpm install --frozen-lockfile \
    && pnpm -F @vben/web-antd run build

FROM golang:1.26-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/frontend/apps/web-antd/dist ./frontend/apps/web-antd/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -o /out/ai-upstream-monitor .

FROM alpine:3.22

RUN adduser -D -H app
WORKDIR /app
COPY --from=builder /out/ai-upstream-monitor /app/ai-upstream-monitor
RUN mkdir -p /app/pb_data && chown -R app:app /app

USER app
EXPOSE 8090
CMD ["/app/ai-upstream-monitor", "serve", "--http", "0.0.0.0:8090"]
