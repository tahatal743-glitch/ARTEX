#!/bin/sh
# ============================================================
# ARTEX 容器工具补装 · 首轮（apt 系统包 + pip 工具 + GitHub Release 二进制 + 脚本类）
#
# 用途：补齐 88 条内置工具配方中已被绑定使用的命令。
# 调用：由 Dockerfile / Dockerfile.runtime 在构建期自动执行；也可在容器内手动重跑：
#   GH_TOKEN=<pat> sh docker/tool-install.sh
#   - GH_TOKEN 仅用于提高 GitHub API 限额，不落盘、不进镜像（构建期走 BuildKit secret）。
# 目标架构由 ARTEX_TARGET_ARCH 指定（amd64 / arm64），缺省取 uname -m。
# 幂等，可重复执行；各步失败不中断，末尾输出 command -v 汇总。
#
# 明确缓装（见汇总）：metasploit（需整个 MSF 框架）、zap（需 ZAP 引擎+Java）、
#   fofa/quake/shodan/virustotal/zoomeye（需 API key）、lightx（来源待确认）、
#   x8（官方无预编译包，改由 tool-rust.sh 从源码编译）。
# ============================================================
set -u
export DEBIAN_FRONTEND=noninteractive
# 别让 git 在拿不到凭据时挂住等输入：网络被中间拦截时 GitHub 会回 401，直接失败重试即可。
export GIT_TERMINAL_PROMPT=0
PASS=""; FAIL=""
note_ok(){ PASS="$PASS $1"; echo "  [OK]   $1"; }
note_bad(){ FAIL="$FAIL $1"; echo "  [FAIL] $1"; }
have(){ command -v "$1" >/dev/null 2>&1; }
ghcurl(){ if [ -n "${GH_TOKEN:-}" ]; then curl -H "Authorization: Bearer ${GH_TOKEN}" "$@"; else curl "$@"; fi; }
# GitHub 直连在国内会间歇性被拦（401 / 连接重置），克隆统一走三次重试
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

# 目标架构 → GitHub 资产名匹配正则（amd64/x86_64 与 arm64/aarch64 是两套常见写法）
ARCH="${ARTEX_TARGET_ARCH:-$(uname -m)}"
case "$ARCH" in
  amd64|x86_64)  ARCH_RE='(amd64|x86_64)' ;;
  arm64|aarch64) ARCH_RE='(arm64|aarch64|arm64v8)' ;;
  *)             ARCH_RE="($ARCH)" ;;
esac
echo "目标架构: $ARCH (匹配 $ARCH_RE)"

echo "===== 1/6 apt 系统包 ====="
timeout 900 apt-get update -qq || echo "apt update 超时或失败（继续）"
APT_PKGS="sqlmap whatweb hydra binwalk masscan arp-scan nbtscan john hashcat foremost \
libimage-exiftool-perl binutils bsdextrautils xxd gobuster steghide smbclient \
perl ruby ruby-dev build-essential g++ libcurl4-openssl-dev pkg-config libssl-dev \
libnet-dns-perl libnet-ip-perl libnet-netmask-perl libnet-whois-ip-perl libxml-writer-perl \
libjson-perl libswitch-perl libxml2-dev libxslt1-dev zlib1g-dev"
# 说明：ruby-dev 供 wpscan 的原生扩展（yajl-ruby）编译；pkg-config + libssl-dev 供 x8
# 的 Rust 构建（openssl-sys）使用。缺任一项，对应工具会静默构建失败。
if timeout 1500 apt-get install -y --no-install-recommends $APT_PKGS; then
  echo "apt 批量安装完成"
else
  echo "apt 批量有失败，逐包重试"
  for p in $APT_PKGS; do timeout 300 apt-get install -y --no-install-recommends "$p" >/dev/null 2>&1 || note_bad "apt:$p"; done
fi

echo "===== 2/6 pip Python 工具 ====="
pip1(){ timeout 420 python3 -m pip install --no-cache-dir -q "$1" && note_ok "pip:$1" || note_bad "pip:$1"; }
pip1 arjun; pip1 wafw00f; pip1 dirsearch; pip1 fierce; pip1 graphqlmap
pip1 smbmap; pip1 netexec; pip1 bloodhound-python
pip1 requests; pip1 charset-normalizer; pip1 httpx; pip1 pycurl
if ! timeout 420 python3 -m pip install --no-cache-dir -q paramspider; then
  echo "paramspider pip 失败，改 GitHub 源码安装"
  git_clone https://github.com/devanshbatham/ParamSpider /opt/paramspider && \
  timeout 300 python3 -m pip install --no-cache-dir -q /opt/paramspider && note_ok paramspider || note_bad paramspider
else note_ok "pip:paramspider"; fi
if ! timeout 420 python3 -m pip install --no-cache-dir -q jwt_tool; then
  echo "jwt_tool pip 失败，改 GitHub 源码安装"
  git_clone https://github.com/ticarpi/jwt_tool /opt/jwt_tool && \
  timeout 300 python3 -m pip install --no-cache-dir -q -r /opt/jwt_tool/requirements.txt && \
  printf '#!/bin/sh\nexec python3 /opt/jwt_tool/jwt_tool.py "$@"\n' > /usr/local/bin/jwt_tool && chmod +x /usr/local/bin/jwt_tool && note_ok jwt_tool || note_bad jwt_tool
else note_ok "pip:jwt_tool"; fi

echo "===== 3/6 GitHub Release 二进制 ====="
gh_bin(){ # $1 repo  $2 安装名  [$3 包内二进制名]
  r="$1"; b="$2"; inner="${3:-$2}"
  rm -rf /tmp/arti; mkdir -p /tmp/arti
  url=$(ghcurl -fsSL --max-time 30 "https://api.github.com/repos/$r/releases/latest" 2>/dev/null | ARCH_RE="$ARCH_RE" python3 -c 'import json,os,sys,re
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
pat=re.compile(r"linux[._-].*"+os.environ["ARCH_RE"]+r"|"+os.environ["ARCH_RE"]+r".*linux", re.I)
for x in d.get("assets",[]):
    n=x["name"].lower()
    if re.search(r"(sha256|sums|\.sig$|\.asc$|\.txt$)", n): continue
    if pat.search(x["name"]): print(x["browser_download_url"]); break')
  # 直连被拦时 API 会返回空；重试两次再判「无匹配资产」
  _try=1
  while [ -z "$url" ] && [ "$_try" -le 3 ]; do
    sleep 2
    url=$(ghcurl -fsSL --max-time 30 "https://api.github.com/repos/$r/releases/latest" 2>/dev/null | ARCH_RE="$ARCH_RE" python3 -c 'import json,os,sys,re
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
pat=re.compile(r"linux[._-].*"+os.environ["ARCH_RE"]+r"|"+os.environ["ARCH_RE"]+r".*linux", re.I)
for x in d.get("assets",[]):
    n=x["name"].lower()
    if re.search(r"(sha256|sums|\.sig$|\.asc$|\.txt$)", n): continue
    if pat.search(x["name"]): print(x["browser_download_url"]); break')
    _try=$((_try+1))
  done
  if [ -z "$url" ]; then note_bad "$b(无匹配资产)"; return 1; fi
  case "$url" in
    *.deb)
      curl -fsSL --max-time 300 -o /tmp/arti.deb "$url" || { note_bad "$b(下载失败)"; return 1; }
      if dpkg -i /tmp/arti.deb >/dev/null 2>&1; then note_ok "$b(deb)"; return 0; fi
      dpkg-deb -x /tmp/arti.deb /tmp/arti 2>/dev/null
      f=$(find /tmp/arti -type f -name "$inner" | head -1)
      if [ -n "$f" ]; then install -m 0755 "$f" "/usr/local/bin/$b" && note_ok "$b(deb-extract)" || note_bad "$b"; else note_bad "$b(deb)"; fi
      return 0 ;;
    *.zip) curl -fsSL --max-time 300 -o /tmp/arti.zip "$url" && unzip -oq /tmp/arti.zip -d /tmp/arti ;;
    *) curl -fsSL --max-time 300 -o /tmp/arti.tgz "$url" && { tar xzf /tmp/arti.tgz -C /tmp/arti 2>/dev/null || tar xjf /tmp/arti.tgz -C /tmp/arti 2>/dev/null || tar xJf /tmp/arti.tgz -C /tmp/arti 2>/dev/null; } ;;
  esac
  f=$(find /tmp/arti -type f -name "$inner" | head -1)
  if [ -n "$f" ]; then install -m 0755 "$f" "/usr/local/bin/$b" && note_ok "$b" || note_bad "$b(安装失败)"; else note_bad "$b(包内未找到 $inner)"; fi
}
gh_bin ffuf/ffuf ffuf
gh_bin hahwul/dalfox dalfox
gh_bin epi052/feroxbuster feroxbuster
gh_bin owasp-amass/amass amass
gh_bin projectdiscovery/nuclei nuclei
gh_bin projectdiscovery/httpx httpx-toolkit httpx
gh_bin projectdiscovery/subfinder subfinder
gh_bin projectdiscovery/katana katana
gh_bin lc/gau gau
gh_bin jaeles-project/jaeles jaeles
gh_bin rustscan/rustscan rustscan
gh_bin Sh1Yo/x8 x8
gh_bin tomnomnom/waybackurls waybackurls

# Go 兜底：官方无本架构预编译包的工具（waybackurls / jaeles），用 Go 工具链编译
if ! have waybackurls || ! have jaeles; then
  echo "-- Go 兜底编译（waybackurls / jaeles）--"
  timeout 600 apt-get install -y --no-install-recommends golang-go >/dev/null 2>&1
  export GOPROXY="https://goproxy.cn,direct" GOBIN=/usr/local/bin
  if have go; then
    if ! have waybackurls; then timeout 420 go install github.com/tomnomnom/waybackurls@latest && note_ok "waybackurls(go)" || note_bad "waybackurls(go)"; fi
    if ! have jaeles; then timeout 600 go install github.com/jaeles-project/jaeles@latest && note_ok "jaeles(go)" || note_bad "jaeles(go)"; fi
  else
    note_bad "go-toolchain"
  fi
fi

echo "===== 4/6 脚本类工具 ====="
git_clone https://github.com/sullo/nikto /opt/nikto
if [ -f /opt/nikto/program/nikto.pl ]; then printf '#!/bin/sh\nexec perl /opt/nikto/program/nikto.pl "$@"\n' > /usr/local/bin/nikto && chmod +x /usr/local/bin/nikto && note_ok nikto || note_bad nikto; else note_bad nikto; fi
git_clone https://github.com/fwaeytens/dnsenum /opt/dnsenum
if [ -f /opt/dnsenum/dnsenum.pl ]; then printf '#!/bin/sh\nexec perl /opt/dnsenum/dnsenum.pl "$@"\n' > /usr/local/bin/dnsenum && chmod +x /usr/local/bin/dnsenum && note_ok dnsenum || note_bad dnsenum; else note_bad dnsenum; fi
git_clone https://github.com/wireghoul/dotdotpwn /opt/dotdotpwn
if [ -f /opt/dotdotpwn/dotdotpwn.pl ]; then printf '#!/bin/sh\ncd /opt/dotdotpwn && exec perl dotdotpwn.pl "$@"\n' > /usr/local/bin/dotdotpwn && chmod +x /usr/local/bin/dotdotpwn && note_ok dotdotpwn || note_bad dotdotpwn; else note_bad dotdotpwn; fi
# 原仓 bwall/HashPump 已从 GitHub 删除（404，git 会转而索要凭据并失败），改用同一份代码的
# 存活镜像；该仓没有 makefile，直接 g++ 编译（OpenSSL 3 下只有弃用告警，不影响功能）。
git_clone https://github.com/miekrr/HashPump /opt/hashpump
if [ -d /opt/hashpump ]; then
  (cd /opt/hashpump && g++ -O2 -o hashpump main.cpp Extender.cpp MD4ex.cpp MD5ex.cpp SHA1.cpp SHA256.cpp SHA512ex.cpp -lcrypto >/dev/null 2>&1)
  if [ -x /opt/hashpump/hashpump ]; then cp /opt/hashpump/hashpump /usr/local/bin/ && note_ok hashpump || note_bad hashpump; else note_bad hashpump; fi
else note_bad hashpump; fi
if curl -fsSL --max-time 60 -o /usr/local/bin/checksec https://raw.githubusercontent.com/slimm609/checksec.sh/master/checksec && chmod +x /usr/local/bin/checksec; then note_ok checksec; else
  git_clone https://github.com/slimm609/checksec.sh /opt/checksec
  if [ -f /opt/checksec/checksec ]; then cp /opt/checksec/checksec /usr/local/bin/ && chmod +x /usr/local/bin/checksec && note_ok checksec || note_bad checksec; else note_bad checksec; fi
fi
git_clone https://github.com/epsylon/xsser /opt/xsser
if [ -d /opt/xsser ]; then
  ENTRY=$(ls /opt/xsser/xsser.py /opt/xsser/xsser 2>/dev/null | head -1)
  if [ -n "$ENTRY" ]; then printf '#!/bin/sh\ncd /opt/xsser && exec python3 %s "$@"\n' "$(basename "$ENTRY")" > /usr/local/bin/xsser && chmod +x /usr/local/bin/xsser && note_ok xsser || note_bad xsser; else note_bad xsser; fi
else note_bad xsser; fi

echo "===== 5/6 npm / gem ====="
if timeout 300 npm install -g --no-fund --no-audit --registry=https://registry.npmmirror.com @stoplight/spectral-cli >/dev/null 2>&1; then note_ok spectral; else
  timeout 300 npm install -g --no-fund --no-audit @stoplight/spectral-cli >/dev/null 2>&1 && note_ok spectral || note_bad spectral
fi
timeout 900 gem install wpscan --no-document >/dev/null 2>&1 && note_ok wpscan || note_bad wpscan

echo "===== 6/6 汇总 ====="
echo "--- 命令存在性验证 ---"
for c in nmap sqlmap whatweb hydra binwalk masscan arp-scan nbtscan john hashcat foremost \
         exiftool strings objdump xxd gobuster steghide smbclient perl ruby \
         dnsenum nikto dotdotpwn checksec hashpump xsser \
         amass dalfox feroxbuster ffuf httpx-toolkit subfinder katana nuclei gau jaeles rustscan x8 waybackurls \
         spectral arjun wafw00f dirsearch fierce paramspider graphqlmap smbmap netexec bloodhound-python jwt_tool \
         wpscan; do
  if have "$c"; then echo "  OK   $c -> $(command -v $c)"; else echo "  MISS $c"; fi
done
echo "--- python3 模块 ---"
python3 - <<'PY'
mods = ["requests", "httpx", "charset_normalizer", "pycurl"]
for m in mods:
    try:
        __import__(m); print("  OK   py:" + m)
    except Exception as e:
        print("  MISS py:" + m, "->", e)
PY
echo "--- hashpump 自检（算法实现）---"
hashpump -t 2>&1 | tail -6
echo "--- 版本抽检 ---"
httpx-toolkit -version 2>&1 | head -1
ffuf -V 2>&1 | head -1
nuclei -version 2>&1 | head -2
sqlmap --version 2>&1 | head -1
gobuster version 2>&1 | head -1
echo "===== PASS:$PASS ====="
echo "===== FAIL:$FAIL ====="
apt-get clean >/dev/null 2>&1; rm -rf /var/lib/apt/lists/* /tmp/arti* /root/go/pkg/mod /root/.cache/go-build 2>/dev/null
echo "===== DONE ====="
