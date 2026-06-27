FROM node:24-alpine AS frontend-builder

ARG VITE_BUILD_VERSION=dev
ENV VITE_BUILD_VERSION=$VITE_BUILD_VERSION
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY frontend ./
RUN pnpm build

FROM golang:1.26-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -buildvcs=false -o /out/ai-upstream-monitor .

FROM alpine:3.22

RUN adduser -D -H app
WORKDIR /app
COPY --from=builder /out/ai-upstream-monitor /app/ai-upstream-monitor
RUN mkdir -p /app/data /app/pb_data && chown -R app:app /app
USER app
EXPOSE 8090
CMD ["/app/ai-upstream-monitor"]
