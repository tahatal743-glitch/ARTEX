#!/bin/sh
# ARTEX 容器工具补装 · 补漏轮
# 处理首轮遗留：hashpump、bloodhound-python（PyPI 占位包）、graphqlmap、netexec、wpscan，
# 以及功能抽检发现的：dnsenum/dotdotpwn 缺 Perl 模块、xsser 包装缺 cd、checksec 缺 file。
# 另含 FIX 6：cryptography / PyYAML 的钉版与源码重建（见该节注释）。
# 由 Dockerfile / Dockerfile.runtime 在构建期自动执行；幂等，可重复运行。
# 目标架构由 ARTEX_TARGET_ARCH 指定（amd64 / arm64），缺省取 uname -m。
set -u
export DEBIAN_FRONTEND=noninteractive
# 网络被中间拦截时 GitHub 会回 401，git 会去要凭据：关掉交互，统一重试。
export GIT_TERMINAL_PROMPT=0
# GitHub 直连在国内会间歇性被拦，克隆统一走三次重试
git_clone(){ # $1 url  $2 目标目录
  _u="$1"; _d="$2"; _try=1
  while [ "$_try" -le 3 ]; do
    rm -rf "$_d"
    if timeout 180 git clone --depth 1 "$_u" "$_d" >/dev/null 2>&1; then return 0; fi
    echo "  [retry $_try/3] git clone 失败：$_u"
    _try=$((_try+1)); sleep 3
  done
  return 1
}

echo "=== FIX 0: 补齐 Perl 模块与 file 工具 ==="
timeout 600 apt-get update -qq
timeout 600 apt-get install -y --no-install-recommends \
  libstring-random-perl libwww-perl libio-socket-ssl-perl libnet-ssleay-perl file 2>&1 | tail -3
echo "[dnsenum 复检]"; timeout 30 dnsenum --help 2>&1 | head -3
echo "[dotdotpwn 复检]"; timeout 30 dotdotpwn --help 2>&1 | head -3
echo "[checksec 复检]"; timeout 30 checksec --version 2>&1 | head -2

echo; echo "=== FIX 0b: xsser 包装修正（cd 到安装目录）==="
if [ -f /opt/xsser/xsser.py ]; then
  printf '#!/bin/sh\ncd /opt/xsser && exec python3 xsser.py "$@"\n' > /usr/local/bin/xsser && chmod +x /usr/local/bin/xsser && echo "xsser 包装已修正（xsser.py）"
elif [ -f /opt/xsser/xsser ]; then
  printf '#!/bin/sh\ncd /opt/xsser && exec python3 xsser "$@"\n' > /usr/local/bin/xsser && chmod +x /usr/local/bin/xsser && echo "xsser 包装已修正（xsser）"
else echo "xsser 入口不存在"; fi
timeout 30 xsser --help 2>&1 | head -3

echo; echo "=== FIX 1: hashpump（重试 clone + g++ 编译）==="
# 原仓 bwall/HashPump 已 404，用同一份代码的存活镜像；仓库无 makefile，直接 g++。
git_clone https://github.com/miekrr/HashPump /opt/hashpump
if [ -d /opt/hashpump ]; then
  (cd /opt/hashpump && g++ -O2 -o hashpump main.cpp Extender.cpp MD4ex.cpp MD5ex.cpp SHA1.cpp SHA256.cpp SHA512ex.cpp -lcrypto 2>&1 | tail -3)
  if [ -x /opt/hashpump/hashpump ]; then cp /opt/hashpump/hashpump /usr/local/bin/ && echo "hashpump -> OK" || echo "hashpump -> cp 失败"; else echo "hashpump -> 编译未产出二进制"; fi
else echo "hashpump -> clone 失败"; fi

echo; echo "=== FIX 2: bloodhound（真包 PyPI 名为 bloodhound，卸掉占位包）==="
python3 -m pip uninstall -y -q bloodhound-python 2>&1 | tail -1
timeout 420 python3 -m pip install --no-cache-dir bloodhound 2>&1 | tail -3
echo "bloodhound-python -> $(command -v bloodhound-python || echo MISS)"

echo; echo "=== FIX 3: graphqlmap（GitHub swisskyrepo/GraphQLmap）==="
git_clone https://github.com/swisskyrepo/GraphQLmap /opt/graphqlmap
if [ -d /opt/graphqlmap ]; then
  echo "仓库文件:"; ls /opt/graphqlmap | head -14
  timeout 200 python3 -m pip install --no-cache-dir -q requests websocket-client 2>&1 | tail -1
  ENTRY=$(ls /opt/graphqlmap/graphqlmap.py /opt/graphqlmap/graphqlmap 2>/dev/null | head -1)
  if [ -n "$ENTRY" ]; then
    printf '#!/bin/sh\nexec python3 %s "$@"\n' "$ENTRY" > /usr/local/bin/graphqlmap
    chmod +x /usr/local/bin/graphqlmap
    echo "graphqlmap -> OK（入口 $ENTRY）"
  else echo "graphqlmap -> 未找到入口文件"; fi
else echo "graphqlmap -> clone 失败"; fi

echo; echo "=== FIX 4: netexec（GitHub Pennyw0rth/NetExec）==="
timeout 900 python3 -m pip install --no-cache-dir "git+https://github.com/Pennyw0rth/NetExec" 2>&1 | tail -5
if command -v nxc >/dev/null 2>&1 && ! command -v netexec >/dev/null 2>&1; then
  ln -sf "$(command -v nxc)" /usr/local/bin/netexec && echo "netexec -> 经 nxc 建链"
fi
echo "nxc -> $(command -v nxc || echo MISS)"
echo "netexec -> $(command -v netexec || echo MISS)"

echo; echo "=== FIX 5: wpscan（gem 走国内镜像）==="
gem sources --add https://mirrors.tuna.tsinghua.edu.cn/rubygems/ --remove https://rubygems.org/ 2>&1 | tail -4
timeout 1200 gem install wpscan --no-document 2>&1 | tail -5
echo "wpscan -> $(command -v wpscan || echo MISS)"

echo; echo "=== FIX 6: cryptography / PyYAML 钉版（部分虚拟化 CPU 上的 SIGILL 兜底）==="
# 新版 cryptography 的 Rust 扩展与 PyYAML 预编译 C 扩展，在部分虚拟化 CPU（如 Docker Desktop
# 在 Apple Silicon 上的虚拟 CPU）上会 SIGILL（Illegal instruction），表现为 import 即崩。
# cryptography 42.0.8 实测可用（x509/Fernet/RSA/bloodhound 均通过）；PyYAML 改源码重建绕开预编译扩展。
# 该钉版对普通 x86_64 环境同样无害（只是版本确定性），勿升级 cryptography。
timeout 420 python3 -m pip install --no-cache-dir -q "cryptography==42.0.8" 2>&1 | tail -2
python3 -c "import cryptography; print('crypto', cryptography.__version__)" 2>&1 | tail -1
timeout 420 python3 -m pip install --no-cache-dir --force-reinstall --no-binary pyyaml pyyaml 2>&1 | tail -2
python3 -c "import yaml; print('yaml', yaml.__version__)" 2>&1 | tail -1
python3 -c "import cryptography.x509; print('x509 ok')" 2>&1 | tail -1

echo; echo "=== httpx-toolkit 版本复核（应为 ProjectDiscovery）==="
httpx-toolkit -version 2>&1 | head -3

echo; echo "=== 最终验证 ==="
for c in hashpump bloodhound-python graphqlmap netexec wpscan; do
  if command -v "$c" >/dev/null 2>&1; then echo "OK   $c -> $(command -v $c)"; else echo "MISS $c"; fi
done
echo "--- 功能复检 ---"
echo "[bloodhound-python]"; timeout 30 bloodhound-python --help 2>&1 | head -2
echo "[netexec]"; timeout 60 netexec --version 2>&1 | head -2
echo "[graphqlmap]"; timeout 30 graphqlmap 2>&1 | head -3
echo "[wpscan]"; timeout 60 wpscan --version 2>&1 | head -3

apt-get clean >/dev/null 2>&1; rm -rf /var/lib/apt/lists/* 2>/dev/null
echo "===== FIXES DONE ====="
