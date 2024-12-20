# 使用官方 Go 镜像作为构建环境
FROM golang:1.22 AS builder

# 设置工作目录
WORKDIR /app

# 将 go.mod 和 go.sum 文件复制到工作目录，并下载依赖
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

# 复制项目的全部代码到工作目录
COPY . .

# 编译可执行文件，确保它适用于 Linux 并禁用 CGO
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o main .

# 使用一个更小的镜像来运行构建的应用程序
FROM alpine:latest

# 安装运行依赖
RUN apk --no-cache add ca-certificates

# 设置工作目录
WORKDIR /app

# 创建 uploads 目录
RUN mkdir -p /app/uploads

# 复制编译后的二进制文件到 /app 目录
COPY --from=builder /app/main /app/main

# 暴露端口
EXPOSE 4000

# 运行应用程序
CMD ["./main"]