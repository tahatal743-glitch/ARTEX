# 镜像构建：三条路线

仓库提供两份 Dockerfile，产物功能一致（同样的运行层、同样的工具层），区别只在**二进制从哪来**。

| | 路线 A：预编译二进制 | 路线 B：全源码 |
|---|---|---|
| Dockerfile | `Dockerfile.runtime` | `Dockerfile`（根目录） |
| 输入 | Release zip 里的 `artex` 单二进制 → `dist/<arch>/artex` | `web/` + Go 源码 |
| 构建耗时 | 只装工具层（apt + pip + 二进制下载 + Rust 现编） | 前端 `npm ci` + 静态导出 + Go 交叉编译 + 工具层 |
| 适用 | 新机器快速部署、只换程序不换工具链版本 | 改了源码/前端后自建镜像 |
| 前置 | Docker Desktop（BuildKit）、Release zip | Docker Desktop、可访问 npm/Go 源 |

两条路线产出的镜像都**自带工具层**：88 条内置工具配方所需的命令（apt 系统包、pip 工具、
GitHub Release 二进制、perl/python 脚本、Rust 现编的 aardwolf 与 x8）在构建期一次性装好，
不必再逐台手工补装。工具明细与缓装清单见 `tool-install.sh` / `tool-fix.sh` / `tool-rust.sh`
的头部注释与末尾汇总输出。

---

## 路线 0：直接拉 ghcr 预构建镜像（新机器最快）

每次推 `v*` tag，仓库 Actions 会自动构建**带完整工具层**的镜像并发布到 GitHub Container Registry，
新机器连构建都省：

```bash
docker pull ghcr.io/tahatal743-glitch/artex:<版本>   # 例如 v0.3.22
# 或跟随最新：
docker pull ghcr.io/tahatal743-glitch/artex:latest
```

- 多架构 manifest（linux/amd64 + linux/arm64），`docker pull` 按本机架构自动选择。
- 与 Release zip 同源：二进制取自同一次 tag 构建的 Linux 产物；工具层与路线 A/B 完全一致。
- **tag 发版后镜像还需要约 15~25 分钟构建**（实测含冷构建约 15 分钟，视缓存；工具层含 Rust 现编），稍等再拉。
- 起容器沿用既有 compose，把 `image:` 换成 `ghcr.io/tahatal743-glitch/artex:<版本>` 即可；
  固定版本用具体 tag，自动跟随用 `latest`。
- 包页面：`https://github.com/tahatal743-glitch/ARTEX/pkgs/container/artex`
- 若包不可匿名拉取（或使用私有 fork），先登录：
  `echo <token> | docker login ghcr.io -u <用户名> --password-stdin`（token 需 `read:packages`）。

拉完自检与「构建后自检」一节相同（把镜像名换成 ghcr 的即可）。

---

## 路线 A：预编译二进制（推荐给新机器）

```bash
# 0) 取 Release 二进制（按机器架构选：Apple Silicon → linux-arm64，Intel → linux-amd64）
unzip -o artex-<版本>-linux-arm64.zip -d /tmp/artex-rel

# 1) 放进构建上下文的 dist/<arch>/
mkdir -p dist/arm64
cp /tmp/artex-rel/artex-<版本>-linux-arm64/artex dist/arm64/artex

# 2) 构建（可选：用 BuildKit secret 传 GitHub token 提高 API 限额）
docker build -f Dockerfile.runtime -t artex:<版本>-local .
# 带 token：
docker build -f Dockerfile.runtime \
  --secret id=gh_token,src=/path/to/gh_token \
  -t artex:<版本>-local .

# 3) 起容器（沿用现有 compose 的挂载/端口/env，只换 image）
docker compose up -d
```

`dist/` 在 `.gitignore` 里，二进制不会进仓库；`Dockerfile.runtime.dockerignore` 是配套白名单，
构建上下文只包含 `Dockerfile.runtime`、`start.sh`、`docker/`、`dist/<arch>/artex`、`skills/`。

> `TARGETARCH` 由 BuildKit 自动注入（Apple Silicon 上是 `arm64`，Intel 上是 `amd64`），
> 无需手工指定。若报 `dist//artex not found`，说明用了不带 BuildKit 的旧构建器。

## 路线 B：全源码

```bash
docker compose build        # 等价于 docker build -t artex:local .
# 国内网络可覆盖依赖源：
#   --build-arg GOPROXY=https://goproxy.cn,direct
#   --build-arg NPM_REGISTRY=https://registry.npmmirror.com
```

前端静态导出 → Go 静态编译（内嵌前端）→ 运行镜像 + 工具层，全部在构建期完成。

---

## 浏览器下载源（Playwright / chromium）

运行镜像预装 chromium，构建期从 `PLAYWRIGHT_DOWNLOAD_HOST` 拉取，默认
`https://cdn.npmmirror.com/binaries/playwright`，失败会自动回落官方 CDN。
**镜像站只同步了 x64 的 cft 构建，arm64 会 404**，所以：

- amd64 构建：默认参数即可。
- arm64 构建（Apple Silicon）：镜像站缺件 → 回落官方 CDN；官方 CDN 若也不通，构建失败并提示。
  自建镜像目录是最可靠的做法：

```bash
# ① 在能访问官方 CDN 的机器上，按原路径准备三个归档（版本见 Dockerfile 的 ARG）
#    builds/cft/<chrome-version>/linux-arm64/chrome-linux-arm64.zip
#    builds/cft/<chrome-version>/linux-arm64/chrome-headless-shell-linux-arm64.zip
#    builds/ffmpeg/1011/ffmpeg-linux-arm64.zip
# ② 起个静态服务当下载源
python3 -m http.server 18099 --directory /path/to/browser-downloads
# ③ 构建时指过来（容器内访问宿主机用 host.docker.internal）
docker build -f Dockerfile.runtime \
  --build-arg PLAYWRIGHT_DOWNLOAD_HOST=http://host.docker.internal:18099 \
  -t artex:<版本>-local .
```

版本配套（`Dockerfile` 的 `ARG PLAYWRIGHT_*`）：`playwright` 决定要哪个 chromium 构建，
`@playwright/mcp` / `@playwright/cli` 是配套的 MCP 与 CLI，**三者要一起升级、一起验**，
否则会出现版本配套不上的下载 404。当前钉的组合：playwright `1.64.0-alpha-1790635538000`
（chromium 1247 / Chrome 155.0.8059.12）、`@playwright/mcp 0.0.83`、`@playwright/cli 0.1.22`。

## 工具层

三个脚本幂等、可重复执行，构建期自动跑，也可在容器内手动重跑：

```bash
docker exec -it <容器名> sh -c 'GH_TOKEN=<pat> sh /tmp/tool-install.sh'   # 构建后 /tmp 下已清理，需先拷入
```

- `tool-install.sh`：apt 系统包、pip 工具、GitHub Release 二进制、perl/python 脚本类（nikto / dnsenum / dotdotpwn / hashpump / checksec / xsser）、npm+gem（spectral / wpscan）。
- `tool-fix.sh`：首轮遗留与功能抽检的补漏（hashpump、bloodhound 真包名、graphqlmap、netexec、wpscan、Perl 模块），外加 **FIX 6**：`cryptography==42.0.8` 钉版 + `PyYAML` 源码重建。
- `tool-rust.sh`：Rust 工具链（清华镜像）→ aardwolf 扩展、netexec、x8 从源码编译。

**架构**：三脚本按 `ARTEX_TARGET_ARCH`（构建期传 `TARGETARCH`，容器内缺省取 `uname -m`）
匹配 GitHub 资产名与编译目标，amd64 / arm64 均可用。Intel 机器用 `dist/amd64/artex` 即可，
无需改脚本。

**FIX 6 是为什么**：新版 `cryptography` 的 Rust 扩展与 `PyYAML` 预编译 C 扩展，在部分虚拟化 CPU
（例如 Docker Desktop 在 Apple Silicon 上的虚拟 CPU）上会 `SIGILL`（Illegal instruction），
表现为 import 即崩。钉 `cryptography==42.0.8` + PyYAML 源码重建可绕开。**勿升级 cryptography**。

**GitHub 限流**：不传 token 时走匿名 API（每小时 60 次），13 个 `gh_bin` 调用够用但可能偶发失败
（脚本会记录到 `FAIL:` 汇总，不中断构建）。要稳定就传 `--secret id=gh_token,src=<文件>`。

**缓装清单**（构建后会 MISS，属已知项）：`metasploit`（需整个 MSF 框架）、`zap`（需 ZAP 引擎+Java）、
`lightx`（来源待确认）、`fofa`/`quake`/`shodan`/`virustotal`/`zoomeye`（需 API key，工具本身在，缺 key 时调用报错）。

**上游漂移**（改脚本前先确认上游还在）：`hashpump` 的原仓 `bwall/HashPump` 已删除，脚本用同一份代码的存活镜像 `miekrr/HashPump` 并直接 g++ 编译；`x8`、`jaeles`、`waybackurls` 官方无本架构预编译包，走源码编译（`x8` 需 `pkg-config` + `libssl-dev`）。

## 构建后自检

```bash
docker run --rm --entrypoint sh artex:<版本>-local -c '
  for c in nmap sqlmap ffuf nuclei httpx-toolkit netexec x8 nikto wpscan; do
    printf "%-16s %s\n" "$c" "$(command -v $c || echo MISS)"
  done
  python3 -c "import cryptography,yaml; print(cryptography.__version__, yaml.__version__)"
'
```

期望：命令全部有路径；`cryptography 42.0.8`、`yaml 6.x`。

## 更新与持久化

- **只换二进制不换镜像**：`docker cp` 换 `/app/artex` 只改容器可写层，容器重建会退回镜像自带的版本，
  且工具层不受影响。要连镜像一起升级就用 `docker compose pull artex && docker compose up -d artex`
  （compose 的 `image:` 指 ghcr 时即从 ghcr 拉新版本；或按上面重新构建后 `docker compose up -d`）。
- **容器内一键更新**：只换程序，镜像里的工具链不跟着变，属预期行为。
