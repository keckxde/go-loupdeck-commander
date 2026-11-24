#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

export GOPROXY=off
export GOSUMDB=off

PROJECT_NAME="loupedeck-commander"
PRESET="default"

# Defaults (can be overridden by .keckx-release.yml and CLI flags)
DO_CLEAN=false
DO_CONFIGURE=true
DO_BUILD=true
DO_TEST=true
DO_PACK=true

load_keckx_defaults() {
    local cfg_file="$SCRIPT_DIR/.keckx-release.yml"
    [[ -f "$cfg_file" ]] || return 0

    local in_build=false
    local line stripped key val
    while IFS= read -r line || [[ -n "$line" ]]; do
        stripped="${line#"${line%%[![:space:]]*}"}"
        stripped="${stripped%"${stripped##*[![:space:]]}"}"
        [[ -z "$stripped" || "$stripped" == \#* ]] && continue

        if [[ "$stripped" == "build:" ]]; then
            in_build=true
            continue
        fi

        if [[ "$in_build" == true ]]; then
            if [[ "$line" != [[:space:]]* && "$stripped" == *: ]]; then
                in_build=false
                continue
            fi
            if [[ "$stripped" == *:* ]]; then
                key="${stripped%%:*}"
                val="${stripped#*:}"
                key="${key//[[:space:]]/}"
                val="${val#"${val%%[![:space:]]*}"}"
                val="${val%"${val##*[![:space:]]}"}"
                val="${val,,}"
                case "$val" in
                    true|1|yes) val=true ;;
                    false|0|no) val=false ;;
                    *) continue ;;
                esac
                case "$key" in
                    clean) DO_CLEAN="$val" ;;
                    configure) DO_CONFIGURE="$val" ;;
                    build) DO_BUILD="$val" ;;
                    test) DO_TEST="$val" ;;
                    pack) DO_PACK="$val" ;;
                esac
            fi
        fi
    done < "$cfg_file"
}

show_help() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS] [PRESET]

Build, test, and package ${PROJECT_NAME} via CMake/CTest/CPack presets.

Options:
  clean, --clean, -c      Clean build and dist directories before building
  --no-clean              Do not clean before building
  --configure             Run CMake configure step
  --no-configure          Skip CMake configure step
  --build                 Run CMake build step
  --no-build              Skip CMake build step
  --test                  Run CTest test suite
  --no-test               Skip CTest test suite
  --pack                  Run CPack packaging and collect artifacts into dist/
  --no-pack               Skip CPack packaging
  -p, --project NAME      Specify project name (default: ${PROJECT_NAME})
  --preset PRESET         Specify CMake/CTest/CPack preset (default: ${PRESET})
  -h, --help              Show this help message and exit
EOF
}

do_clean() {
    echo "==> Cleaning build and distribution directories..."
    rm -rf \
        "$SCRIPT_DIR/build" \
        "$SCRIPT_DIR/build-RELEASE" \
        "$SCRIPT_DIR/build-DEBUG" \
        "$SCRIPT_DIR/build-RACE" \
        "$SCRIPT_DIR/build-WINDOWS" \
        "$SCRIPT_DIR/build-NINJA" \
        "$SCRIPT_DIR/dist"
    rm -f "$SCRIPT_DIR"/*.deb "$SCRIPT_DIR"/*.tar.gz
}

collect_artifacts() {
    mkdir -p "$SCRIPT_DIR/dist"
    local prev_nullglob
    prev_nullglob="$(shopt -p nullglob || true)"
    shopt -s nullglob

    local dir f
    for dir in \
        "$SCRIPT_DIR/build-RELEASE" \
        "$SCRIPT_DIR/build-DEBUG" \
        "$SCRIPT_DIR/build-WINDOWS" \
        "$SCRIPT_DIR/build-NINJA"
    do
        [[ -d "$dir" ]] || continue
        for f in "$dir"/*.deb "$dir"/*.tar.gz "$dir"/*.zip "$dir"/*.exe; do
            cp -f "$f" "$SCRIPT_DIR/dist/"
        done
    done

    if [[ -d "$SCRIPT_DIR/build-WINDOWS/bin" ]]; then
        for f in "$SCRIPT_DIR/build-WINDOWS/bin"/*.exe; do
            cp -f "$f" "$SCRIPT_DIR/dist/"
        done
    fi

    eval "$prev_nullglob"
}

load_keckx_defaults

while [[ $# -gt 0 ]]; do
    case "$1" in
        clean|--clean|-c)
            DO_CLEAN=true
            shift
            ;;
        --no-clean)
            DO_CLEAN=false
            shift
            ;;
        --configure)
            DO_CONFIGURE=true
            shift
            ;;
        --no-configure)
            DO_CONFIGURE=false
            shift
            ;;
        --build)
            DO_BUILD=true
            shift
            ;;
        --no-build)
            DO_BUILD=false
            shift
            ;;
        --test)
            DO_TEST=true
            shift
            ;;
        --no-test)
            DO_TEST=false
            shift
            ;;
        --pack)
            DO_PACK=true
            shift
            ;;
        --no-pack)
            DO_PACK=false
            shift
            ;;
        --project|-p)
            if [[ $# -lt 2 ]]; then
                echo "Error: $1 requires a value" >&2
                exit 1
            fi
            PROJECT_NAME="$2"
            shift 2
            ;;
        --project=*)
            PROJECT_NAME="${1#*=}"
            shift
            ;;
        --preset)
            if [[ $# -lt 2 ]]; then
                echo "Error: --preset requires a value" >&2
                exit 1
            fi
            PRESET="$2"
            shift 2
            ;;
        --preset=*)
            PRESET="${1#*=}"
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        -*)
            echo "Error: Unknown option: $1" >&2
            show_help >&2
            exit 1
            ;;
        *)
            PRESET="$1"
            shift
            ;;
    esac
done

if [[ "$DO_CLEAN" == true ]]; then
    do_clean
fi

if [[ "$DO_CONFIGURE" == true ]]; then
    echo "==> Configuring ${PROJECT_NAME} via CMake preset '${PRESET}'..."
    cmake --preset "${PRESET}"
fi

if [[ "$DO_BUILD" == true ]]; then
    echo "==> Building ${PROJECT_NAME} via CMake preset '${PRESET}'..."
    cmake --build --preset "${PRESET}"
fi

if [[ "$DO_TEST" == true ]]; then
    echo "==> Testing ${PROJECT_NAME} via CTest preset '${PRESET}'..."
    ctest --preset "${PRESET}" --output-on-failure
fi

if [[ "$DO_PACK" == true ]]; then
    if [[ "${PRESET}" != "race" ]]; then
        echo "==> Packaging ${PROJECT_NAME} via CPack preset '${PRESET}'..."
        cpack --preset "${PRESET}"
        collect_artifacts
    else
        echo "==> Skipping packaging for 'race' preset."
    fi
fi

if [[ -x "$SCRIPT_DIR/loupedeck-commander" ]]; then
    echo "✅ Built: $SCRIPT_DIR/loupedeck-commander ($("$SCRIPT_DIR/loupedeck-commander" --version))"
fi
