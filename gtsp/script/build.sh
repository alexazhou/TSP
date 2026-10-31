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

  echo "🔨 Building ${BINARY_NAME} for ${TARGET_GOOS}/${TARGET_GOARCH} (CGO_ENABLED=${EFFECTIVE_CGO:-default})..."
  echo "   source: ${SOURCE_FILE}"

  if [ -n "${EFFECTIVE_CGO}" ]; then
    env GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" CGO_ENABLED="${EFFECTIVE_CGO}" \
      go build -v -o "${OUTPUT}" "${SOURCE_FILE}"
  else
    env GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" \
      go build -v -o "${OUTPUT}" "${SOURCE_FILE}"
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
  echo "❌ 存在未通过静态链接校验的 linux 产物"
  exit 1
fi

echo "🎉 All builds done."
