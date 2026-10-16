#!/bin/sh
# ARTEX 容器工具补装 · 第三轮（Rust 工具链）
# 装 Rust 工具链（清华镜像），用它解锁 netexec 的 aardwolf 扩展与 x8
# （x8 官方无预编译包，任何架构都从源码编译）。
# 由 Dockerfile / Dockerfile.runtime 在构建期自动执行；幂等，可重复运行。
# 目标架构由 ARTEX_TARGET_ARCH 指定（amd64 / arm64），缺省取 uname -m；
# rustup 安装的是宿主架构工具链，交叉目标无需额外指定。
set -u
export RUSTUP_DIST_SERVER=https://mirrors.tuna.tsinghua.edu.cn/rustup
export RUSTUP_UPDATE_ROOT=https://mirrors.tuna.tsinghua.edu.cn/rustup/rustup

echo "=== 1. 安装 Rust 工具链（清华镜像，minimal）==="
curl -sSf --max-time 120 https://sh.rustup.rs -o /tmp/rustup-init.sh && timeout 900 sh /tmp/rustup-init.sh -y --profile minimal --default-toolchain stable 2>&1 | tail -4
export PATH="$HOME/.cargo/bin:$PATH"
echo "rustc -> $(command -v rustc || echo MISS)"; rustc --version 2>/dev/null; cargo --version 2>/dev/null

echo; echo "=== 2. cargo 走清华 crates 镜像 ==="
mkdir -p "$HOME/.cargo"
printf "[source.crates-io]\nreplace-with = \"tuna\"\n[source.tuna]\nregistry = \"sparse+https://mirrors.tuna.tsinghua.edu.cn/crates.io-index/\"\n" > "$HOME/.cargo/config.toml"
cat "$HOME/.cargo/config.toml"

echo; echo "=== 3. 构建 aardwolf（Rust 扩展）==="
timeout 900 python3 -m pip install --no-cache-dir aardwolf 2>&1 | tail -6
if timeout 30 python3 -c "import aardwolf" 2>/dev/null; then echo "aardwolf -> import OK"; else echo "aardwolf -> 仍失败"; fi

echo; echo "=== 4. 安装 netexec ==="
timeout 900 python3 -m pip install --no-cache-dir "git+https://github.com/Pennyw0rth/NetExec" 2>&1 | tail -6
if command -v nxc >/dev/null 2>&1 && ! command -v netexec >/dev/null 2>&1; then ln -sf "$(command -v nxc)" /usr/local/bin/netexec && echo "netexec -> 经 nxc 建链"; fi
echo "nxc -> $(command -v nxc || echo MISS)"; echo "netexec -> $(command -v netexec || echo MISS)"
command -v netexec >/dev/null 2>&1 && timeout 60 netexec --version 2>&1 | head -3

echo; echo "=== 5. 构建 x8（Rust，从 GitHub 源码）==="
# x8 依赖 openssl-sys：缺 pkg-config / libssl-dev 时 cargo 会以「installing pkg-config」报错退出。
export DEBIAN_FRONTEND=noninteractive
timeout 600 apt-get install -y --no-install-recommends pkg-config libssl-dev build-essential >/dev/null 2>&1
timeout 1500 cargo install --quiet --root /usr/local --git https://github.com/Sh1Yo/x8 2>&1 | tail -5
if command -v x8 >/dev/null 2>&1; then echo "x8 -> OK"; timeout 20 x8 --help 2>&1 | head -4; else echo "x8 -> 仍失败"; fi

echo; echo "===== RUST ROUND DONE ====="
