#!/bin/sh
set -eu

REPO="Mark24Code/cli-reader"
BIN_NAME="cli-reader"
INSTALL_DIR="${CLI_READER_INSTALL_DIR:-$HOME/bin}"
VERSION="${VERSION:-}"

usage() {
  cat <<EOF
用法: install.sh [选项] [安装目录]

从 GitHub Releases 安装 ${BIN_NAME}。

选项:
  -d, --dir <目录>  安装目录（默认: ~/bin）
  -h, --help        显示帮助

环境变量:
  VERSION                 指定版本，例如 v0.4.1（默认取最新 Release）
  CLI_READER_INSTALL_DIR  安装目录（命令行参数优先）

示例:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sh -s -- --dir ~/.local/bin
  VERSION=v0.4.1 sh install.sh ~/bin
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    -d | --dir)
      if [ $# -lt 2 ]; then
        echo "错误: $1 需要一个目录参数" >&2
        exit 1
      fi
      INSTALL_DIR="$2"
      shift 2
      ;;
    -*)
      echo "错误: 未知参数 $1" >&2
      usage >&2
      exit 1
      ;;
    *)
      INSTALL_DIR="$1"
      shift
      ;;
  esac
done

case "$INSTALL_DIR" in
  "~") INSTALL_DIR="$HOME" ;;
  "~/"*) INSTALL_DIR="$HOME/${INSTALL_DIR#\~/}" ;;
esac
case "$INSTALL_DIR" in
  /*) ;;
  *) INSTALL_DIR="$(pwd)/$INSTALL_DIR" ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "错误: 需要 curl 才能下载安装包" >&2
  exit 1
fi
if ! command -v tar >/dev/null 2>&1; then
  echo "错误: 需要 tar 才能解压安装包" >&2
  exit 1
fi

os=$(uname -s)
case "$os" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *)
    echo "错误: 暂不支持 $os，请从 https://github.com/${REPO}/releases 手动下载" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *)
    echo "错误: 暂不支持架构 $arch，请从 https://github.com/${REPO}/releases 手动下载" >&2
    exit 1
    ;;
esac

if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1)
  if [ -z "$VERSION" ]; then
    echo "错误: 无法获取最新版本；可设置 VERSION 环境变量后重试，例如 VERSION=v0.4.1" >&2
    exit 1
  fi
fi

case "$VERSION" in
  v*) tag="$VERSION" ;;
  *) tag="v$VERSION" ;;
esac
ver="${tag#v}"

archive="${BIN_NAME}_${ver}_${os}_${arch}.tar.gz"
base_url="https://github.com/${REPO}/releases/download/${tag}"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/cli-reader-install.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

echo "正在安装 ${BIN_NAME} ${tag} (${os}/${arch})"
echo "下载 ${base_url}/${archive}"
curl -fL --retry 3 -o "$tmp_dir/$archive" "${base_url}/${archive}"

if curl -fsSL --retry 3 -o "$tmp_dir/checksums.txt" "${base_url}/checksums.txt" 2>/dev/null; then
  line=$(grep "  ${archive}$" "$tmp_dir/checksums.txt" | head -n 1 || true)
  if [ -n "$line" ]; then
    expected="${line%%  *}"
    if command -v sha256sum >/dev/null 2>&1; then
      actual=$(sha256sum "$tmp_dir/$archive" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
      actual=$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')
    else
      actual=""
      echo "警告: 没有可用的 sha256 校验工具，跳过校验" >&2
    fi
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      echo "错误: SHA-256 校验失败（期望 $expected，实际 $actual）" >&2
      exit 1
    fi
    if [ -n "$actual" ]; then
      echo "SHA-256 校验通过"
    fi
  else
    echo "警告: checksums.txt 中未找到 ${archive}，跳过校验" >&2
  fi
else
  echo "警告: 无法下载 checksums.txt，跳过校验" >&2
fi

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
if [ ! -f "$tmp_dir/$BIN_NAME" ]; then
  echo "错误: 安装包中未找到 ${BIN_NAME}" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "$tmp_dir/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
else
  cp "$tmp_dir/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
  chmod 0755 "$INSTALL_DIR/$BIN_NAME"
fi

echo
echo "已安装: $INSTALL_DIR/$BIN_NAME"
"$INSTALL_DIR/$BIN_NAME" --version

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo
    echo "提示: $INSTALL_DIR 不在 PATH 中，请将下面一行加入 ~/.zshrc 或 ~/.bashrc："
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
