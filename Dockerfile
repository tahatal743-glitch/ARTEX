# syntax=docker/dockerfile:1
#
# 多阶段构建：前端静态导出 → Go 单二进制（内嵌前端）→ 运行镜像。
# 原 Docker Hub 镜像源（autumn27/artex）已失效，本仓库自此支持从纯源码自助构建：
#   docker compose build        # 或 docker build -t artex:local .
# TARGETARCH 由 buildx 自动注入（amd64/arm64），Go 交叉编译无需 QEMU；
# 运行层只装工具，编译全部在构建阶段完成。
# 国内网络可覆盖模块源（默认已对国内友好）：
#   --build-arg GOPROXY=https://goproxy.cn,direct
#   --build-arg NPM_REGISTRY=https://registry.npmmirror.com

########## 阶段 1：前端静态导出 ##########
FROM node:20-bookworm AS web
ARG NPM_REGISTRY=https://registry.npmmirror.com
WORKDIR /web
# --ignore-scripts：跳过 prepare(husky)——容器里没有 .git，husky 会失败；
# Next.js/Biome 的平台二进制走 optionalDependencies，不依赖生命周期脚本。
COPY web/package.json web/package-lock.json ./
RUN npm config set registry "$NPM_REGISTRY" && npm ci --ignore-scripts
COPY web/ ./
RUN NEXT_EXPORT=1 npm run build:static

########## 阶段 2：Go 后端（内嵌前端，静态编译）##########
FROM golang:1.26-bookworm AS gobuild
ARG TARGETARCH
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 前端静态产物从 web 阶段拷入（.dockerignore 已排除宿主 dist，不会冲突）
COPY --from=web /web/out/ server/webui/dist/
RUN CGO_ENABLED=0 GOARCH="${TARGETARCH}" go build -tags embedui -trimpath -o /out/artex ./cmd/artex

########## 阶段 3：运行镜像（同原版：工具 + Playwright 预装）##########
FROM python:3.12-slim-bookworm
ARG TARGETARCH
ARG NPM_REGISTRY=https://registry.npmmirror.com
# Playwright 相关版本与浏览器下载源。钉版而非 @latest：@latest 会漂移到镜像站尚未同步的
# 浏览器构建，导致构建随机失败（chromium 与 @playwright/mcp/cli 的版本必须配套）。
ARG PLAYWRIGHT_MCP_VERSION=0.0.83
ARG PLAYWRIGHT_CLI_VERSION=0.1.22
ARG PLAYWRIGHT_VERSION=1.64.0-alpha-1790635538000
ARG PLAYWRIGHT_DOWNLOAD_HOST=https://cdn.npmmirror.com/binaries/playwright
# 运行层的全局 npm 装包与 chromium 下载同样走国内源（对全球网络同样可达）。
# 镜像站缺件时（例如 arm64 的 cft 构建未同步）会在下面回落官方 CDN；
# 若两条都不通，可自建镜像目录并用 --build-arg PLAYWRIGHT_DOWNLOAD_HOST= 指过来，
# 见 docker/README.md「浏览器下载源」。
ENV npm_config_registry=$NPM_REGISTRY \
    PLAYWRIGHT_DOWNLOAD_HOST=$PLAYWRIGHT_DOWNLOAD_HOST
# 常用工具：ripgrep / curl / vim，加一批 recon 常备件（按需增删）。
# Node 从 NodeSource 装 20.x：bookworm 自带的 apt nodejs 是 18，Playwright 要求 >=20。
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim git jq unzip \
      dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*
# 预装 Playwright MCP 与 CLI（全局），运行时不再 npx 联网下载。
# @playwright/mcp：browser MCP 直接 `npx @playwright/mcp`（已全局装好，无需 -y/@latest）。
# @playwright/cli：提供 playwright-cli，装完顺带 --help 验证可执行。
# 再装 playwright（提供浏览器管理），装完用 --with-deps 预置 chromium 及其系统依赖，
# 这样容器内 MCP/CLI 首次启动即可用，不再联网下载浏览器。
RUN npm install -g @playwright/mcp@${PLAYWRIGHT_MCP_VERSION} @playwright/cli@${PLAYWRIGHT_CLI_VERSION} playwright@${PLAYWRIGHT_VERSION} \
    && playwright-cli --help \
    && (playwright install --with-deps chromium \
        || { echo ">>> 浏览器下载源缺件，回落官方 CDN 重试"; PLAYWRIGHT_DOWNLOAD_HOST= playwright install --with-deps chromium; }) \
    && rm -rf /var/lib/apt/lists/*
# 工具层：88 条内置配方所需的侦察/利用命令（apt 系统包 + pip 工具 + GitHub Release
# 二进制 + perl/python 脚本类 + Rust 现编的 aardwolf/x8）。三个脚本幂等、可重复执行，
# 按 ARTEX_TARGET_ARCH 选对应架构的发布资产；明细与缓装清单见脚本头部注释。
# GH_TOKEN 可选：走 BuildKit secret（--secret id=gh_token,src=<文件>）只用于提高 GitHub
# API 限额，不落盘、不进镜像层；不传则匿名访问（每小时 60 次限流）。
COPY docker/tool-install.sh docker/tool-fix.sh docker/tool-rust.sh /tmp/
RUN --mount=type=secret,id=gh_token,required=false \
    set -eu; \
    export GH_TOKEN="$(cat /run/secrets/gh_token 2>/dev/null || true)"; \
    ARTEX_TARGET_ARCH="${TARGETARCH}" sh /tmp/tool-install.sh && \
    ARTEX_TARGET_ARCH="${TARGETARCH}" sh /tmp/tool-fix.sh && \
    ARTEX_TARGET_ARCH="${TARGETARCH}" sh /tmp/tool-rust.sh && \
    rm -f /tmp/tool-*.sh
WORKDIR /app
# 构建阶段产出的单二进制（内嵌前端）
COPY --from=gobuild /out/artex /app/artex
# 守护启动脚本：进程退出后按退出码决定是否重新拉起，页面一键更新靠它完成换装。
# 它同时负责把 SIGTERM 转发给 artex —— docker stop 只把信号发给 PID 1，
# 不转发的话 artex 收不到、做不了优雅关闭，10 秒后被 SIGKILL 硬杀。
COPY start.sh /app/start.sh
RUN chmod +x /app/artex /app/start.sh
COPY skills/ /app/skills/
# data/（SQLite + jwt.key）持久化点
VOLUME ["/app/data"]
EXPOSE 8787 8788
ENTRYPOINT ["/app/start.sh"]
CMD ["-addr", ":8787", "-proxy", ":8788"]
