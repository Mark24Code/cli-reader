#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

BUILDINFO="internal/buildinfo/buildinfo.go"

current_version=$(sed -n 's/^var Version = "\(.*\)"$/\1/p' "$BUILDINFO" | head -n 1)
if [ -z "$current_version" ]; then
  echo "错误: 无法从 $BUILDINFO 读取当前版本" >&2
  exit 1
fi
if ! [[ "$current_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "错误: 当前版本不是合法的 x.y.z: $current_version" >&2
  exit 1
fi

echo "当前版本: $current_version"

input="${1:-}"
if [ -z "$input" ]; then
  if [ ! -t 0 ]; then
    echo "错误: 非交互环境下请传入版本号，例如 ./scripts/release.sh patch" >&2
    exit 1
  fi
  printf '请输入新版本号（patch / minor / major，或 x.y.z；回车默认 patch）: '
  read -r input
  input="${input:-patch}"
fi

bump_version() {
  local major minor patch
  IFS=. read -r major minor patch <<<"$1"
  case "$2" in
    major) echo "$((major + 1)).0.0" ;;
    minor) echo "$major.$((minor + 1)).0" ;;
    patch) echo "$major.$minor.$((patch + 1))" ;;
  esac
}

case "$input" in
  major | minor | patch)
    new_version=$(bump_version "$current_version" "$input")
    ;;
  *)
    new_version="${input#v}"
    if ! [[ "$new_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "错误: 版本号必须形如 1.2.3（可带 v 前缀），或 major/minor/patch" >&2
      exit 1
    fi
    ;;
esac

if [ "$new_version" = "$current_version" ]; then
  echo "错误: 新版本与当前版本相同 ($new_version)" >&2
  exit 1
fi

tag="v$new_version"
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "错误: 标签 $tag 已存在" >&2
  exit 1
fi

branch=$(git symbolic-ref --short -q HEAD || true)
if [ -z "$branch" ]; then
  echo "错误: 当前不在分支上（detached HEAD）" >&2
  exit 1
fi

if ! git remote get-url origin >/dev/null 2>&1; then
  echo "错误: 没有配置 origin 远程仓库" >&2
  exit 1
fi

echo
echo "即将发布 cli-epub-reader $tag"
echo "  - 运行 go vet 与全部测试"
echo "  - 更新 $BUILDINFO 中的版本为 $new_version"
echo "  - 提交当前所有改动"
echo "  - 创建并推送标签 $tag 与分支 $branch"
echo

go vet ./...
go test ./...

perl -pi -e "s/^var Version = \"[^\"]*\"/var Version = \"$new_version\"/" "$BUILDINFO"
if ! grep -q "var Version = \"$new_version\"" "$BUILDINFO"; then
  echo "错误: 更新版本失败" >&2
  exit 1
fi

git add -A
git commit -m "release: $tag"
git tag -a "$tag" -m "cli-epub-reader $tag"
git push origin "$branch"
git push origin "$tag"

repo_url=$(git remote get-url origin)
case "$repo_url" in
  git@github.com:*) repo_url="https://github.com/${repo_url#git@github.com:}" ;;
  https://github.com/*) ;;
  *) repo_url="" ;;
esac
if [ -n "$repo_url" ]; then
  repo_url="${repo_url%.git}"
  echo
  echo "已推送 $tag。Release 构建进行中："
  echo "  $repo_url/actions"
  echo "  $repo_url/releases/tag/$tag"
fi
