# 宿舍电费监控 — Go 版多阶段构建
# 构建阶段
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/monitor ./cmd/monitor/ && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/webapp ./cmd/webapp/

# 运行阶段
FROM alpine:3.19

RUN apk add --no-cache tzdata ca-certificates

ENV TZ=Asia/Shanghai \
    USTS_DATA_DIR=/app/data \
    ADMIN_KEY=

# Go 二进制
COPY --from=builder /app/monitor /usr/local/bin/monitor
COPY --from=builder /app/webapp /usr/local/bin/webapp

# 前端静态资源
COPY webapp.html 404.html sw.js manifest.json offline.html /app/
COPY static/ /app/static/
COPY entrypoint.sh /app/
COPY config.example.json /app/

# 数据卷
VOLUME ["/app/data"]
EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]