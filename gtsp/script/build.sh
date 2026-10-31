#!/bin/bash

# gTSP Build Script
#
# 用法:
#   ./script/build.sh                        # 构建当前平台，输出 dist/gtsp
#   ./script/build.sh linux/amd64            # 指定单个平台，输出 dist/gtsp-linux-amd64
#   ./script/build.sh linux/amd64 linux/arm64  # 多平台
#
# 链接方式（容器化需求）:
#   - linux 目标默认 CGO_ENABLED=0，产出**完全静态链接**二进制，不依赖
#     glibc/musl，可放进任意镜像（Alpine / ubuntu:20.04 等）作为主进程运行；
#   - 非 linux 目标沿用外部 CGO_ENABLED（未设置即 Go 默认）；
#   - 如需强制 linux 动态链接做兼容性对比：CGO_ENABLED=1 ./script/build.sh linux/amd64
#
# 裁剪（与 CI 保持一致）:
#   - 默认带 -ldflags="-s -w"（去掉符号表与 DWARF），与 .github/workflows/ 里
#     release / publish-pytspclient / test-pytspclient 的发布参数**完全一致**，
#     保证「本脚本产出 == 发布产物」，避免出现 3MB 差异的另一种二进制；
#   - 需要源码级调试（delve / gdb 单步、看变量）时关闭：
#       STRIP=0 ./script/build.sh linux/amd64
#     panic 栈回溯不受裁剪影响（函数名 / 文件:行号 来自 pclntab，仍保留）。

set -e

# Project root directory
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="${PROJECT_ROOT}/dist"
BINARY_NAME="gtsp"
SOURCE_FILE="${PROJECT_ROOT}/src/main.go"

# 目标平台列表：显式参数优先，否则用当前平台
TARGETS=("$@")
if [ ${#TARGETS[@]} -eq 0 ]; then
  TARGETS=("$(go env GOOS)/$(go env GOARCH)")
fi

is_linux_target() {
  case "$1" in
    linux/*) return 0 ;;
    *) return 1 ;;
  esac
}

# 裁剪开关：默认开启（发布产物口径），STRIP=0 关闭
STRIP="${STRIP:-1}"
case "${STRIP}" in
  1|true|TRUE|yes|YES) STRIP=1 ;;
  0|false|FALSE|no|NO) STRIP=0 ;;
  *)
    echo "❌ 无效 STRIP 值: ${STRIP}（取 0/1）"
    exit 1
    ;;
esac
if [ "${STRIP}" -eq 1 ]; then
  LDFLAGS="-s -w"
  BUILD_ARGS=(-ldflags "${LDFLAGS}")
else
  LDFLAGS=""
  BUILD_ARGS=()
fi

mkdir -p "${DIST_DIR}"
FAILED=0

for TARGET in "${TARGETS[@]}"; do
  TARGET_GOOS="${TARGET%%/*}"
  TARGET_GOARCH="${TARGET#*/}"
  if [ -z "${TARGET_GOOS}" ] || [ -z "${TARGET_GOARCH}" ] || [ "${TARGET_GOOS}" = "${TARGET}" ]; then
    echo "❌ 无效目标: ${TARGET}（期望格式 GOOS/GOARCH，如 linux/amd64）"
    exit 1
  fi

  # 输出路径：仅构建当前平台且只有一个目标时输出 dist/gtsp，其余带平台后缀
  if [ ${#TARGETS[@]} -eq 1 ] \
     && [ "${TARGET_GOOS}" = "$(go env GOOS)" ] \
     && [ "${TARGET_GOARCH}" = "$(go env GOARCH)" ]; then
    OUTPUT="${DIST_DIR}/${BINARY_NAME}"
  else
    OUTPUT="${DIST_DIR}/${BINARY_NAME}-${TARGET_GOOS}-${TARGET_GOARCH}"
    if [ "${TARGET_GOOS}" = "windows" ]; then
      OUTPUT="${OUTPUT}.exe"
    fi
  fi

  # linux 默认静态链接：CGO_ENABLED=0 让 net / os-user 走纯 Go 实现，产出无 INTERP 的静态 ELF
  EFFECTIVE_CGO="${CGO_ENABLED:-}"
  if is_linux_target "${TARGET}" && [ -z "${EFFECTIVE_CGO}" ]; then
    EFFECTIVE_CGO=0
  fi

  echo "🔨 Building ${BINARY_NAME} for ${TARGET_GOOS}/${TARGET_GOARCH} (CGO_ENABLED=${EFFECTIVE_CGO:-default}, strip=${STRIP})..."
  echo "   source: ${SOURCE_FILE}"

  if [ -n "${EFFECTIVE_CGO}" ]; then
    env GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" CGO_ENABLED="${EFFECTIVE_CGO}" \
      go build -v "${BUILD_ARGS[@]}" -o "${OUTPUT}" "${SOURCE_FILE}"
  else
    env GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" \
      go build -v "${BUILD_ARGS[@]}" -o "${OUTPUT}" "${SOURCE_FILE}"
  fi

  # linux 产物强制校验静态链接，防止回归成动态链接
  if is_linux_target "${TARGET}"; then
    if file "${OUTPUT}" | grep -q "statically linked"; then
      echo "✅ 静态链接校验通过"
    else
      echo "❌ ${OUTPUT} 不是静态链接（检查 CGO_ENABLED 是否被外部设为 1）"
      file "${OUTPUT}"
      FAILED=1
      continue
    fi
  fi

  # 裁剪校验：STRIP=1（发布口径）时必须带 stripped，防止 -s -w 被漏掉。
  # 只对 linux 目标断言：发布产物只有 linux；且 macOS 的 file 不标 stripped（Mach-O）。
  # 注意：未裁剪时 file 输出以 "not stripped" 结尾，也含子串 "stripped"，
  # 所以必须显式排除 "not stripped"，否则断言恒真（实测踩过）。
  if [ "${STRIP}" -eq 1 ] && is_linux_target "${TARGET}"; then
    FILE_OUT="$(file "${OUTPUT}")"
    if echo "${FILE_OUT}" | grep -q "stripped" && ! echo "${FILE_OUT}" | grep -q "not stripped"; then
      echo "✅ 裁剪校验通过（stripped）"
    else
      echo "❌ ${OUTPUT} 未裁剪（release 口径必须带 -ldflags=\"-s -w\"）"
      echo "${FILE_OUT}"
      FAILED=1
      continue
    fi
  fi

  echo "✅ Build successful!"
  echo "📍 Binary located at: ${OUTPUT}"
  file "${OUTPUT}"
  ls -lh "${OUTPUT}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${OUTPUT}"
  else
    shasum -a 256 "${OUTPUT}"
  fi
  echo
done

if [ "${FAILED}" -ne 0 ]; then
  echo "❌ 存在未通过产物校验（静态链接 / 裁剪）的 linux 产物"
  exit 1
fi

echo "🎉 All builds done."
