# ==============================================================================
# GitVersion.cmake
#
# Resolves project version metadata from Git tags using `git describe`:
#   - Exact tag (clean):   v1.2.3
#   - Exact tag (dirty):   v1.2.3-dirty
#   - Commits past tag:    v1.2.3-4-g1a2b3c4[-dirty]
#   - Untagged repository: v0.1.0-0-g1a2b3c4[-dirty]
#   - No git repository:   v0.1.0-dev
#
# Sets in caller scope:
#   <PREFIX>_VERSION        Full version string (with leading 'v', e.g. v1.2.3-4-g1a2b3c4)
#   <PREFIX>_VERSION_SEMVER Version string without leading 'v' (e.g. 1.2.3-4-g1a2b3c4)
#   <PREFIX>_VER_MAJOR      Numeric major version (normalized for CMake/CPack)
#   <PREFIX>_VER_MINOR      Numeric minor version (normalized for CMake/CPack)
#   <PREFIX>_VER_PATCH      Numeric patch version (normalized for CMake/CPack)
#   <PREFIX>_VER_LABEL      Suffix label (e.g. -4-g1a2b3c4-dirty or empty on exact tag)
#   <PREFIX>_GIT_COMMIT     Short git commit hash (or "unknown")
#   <PREFIX>_BUILD_TIME     UTC ISO-8601 build timestamp
# ==============================================================================

find_package(Git QUIET)

function(determine_git_version PREFIX SOURCE_DIR)
    set(GIT_VER_MAJOR 0)
    set(GIT_VER_MINOR 1)
    set(GIT_VER_PATCH 0)
    set(GIT_VERSION_LABEL "-dev")
    set(GIT_RAW_TAG "v0.1.0")
    set(GIT_COMMIT_SHA "unknown")
    set(GIT_DESCRIBE_STR "")

    string(TIMESTAMP BUILD_TIMESTAMP "%Y-%m-%dT%H:%M:%SZ" UTC)

    if(GIT_EXECUTABLE)
        # 1. Get short commit hash
        execute_process(
            COMMAND ${GIT_EXECUTABLE} rev-parse --short HEAD
            WORKING_DIRECTORY "${SOURCE_DIR}"
            OUTPUT_VARIABLE GIT_REV_OUT
            RESULT_VARIABLE GIT_REV_RES
            ERROR_QUIET
            OUTPUT_STRIP_TRAILING_WHITESPACE
        )
        if(GIT_REV_RES EQUAL 0 AND NOT "${GIT_REV_OUT}" STREQUAL "")
            set(GIT_COMMIT_SHA "${GIT_REV_OUT}")
        endif()

        # 2. Try git describe matching v* tags first, then any tag
        execute_process(
            COMMAND ${GIT_EXECUTABLE} describe --tags --long --dirty --match "v*"
            WORKING_DIRECTORY "${SOURCE_DIR}"
            OUTPUT_VARIABLE GIT_DESCRIBE_OUT
            RESULT_VARIABLE GIT_DESCRIBE_RES
            ERROR_QUIET
            OUTPUT_STRIP_TRAILING_WHITESPACE
        )
        if(NOT GIT_DESCRIBE_RES EQUAL 0)
            execute_process(
                COMMAND ${GIT_EXECUTABLE} describe --tags --long --dirty
                WORKING_DIRECTORY "${SOURCE_DIR}"
                OUTPUT_VARIABLE GIT_DESCRIBE_OUT
                RESULT_VARIABLE GIT_DESCRIBE_RES
                ERROR_QUIET
                OUTPUT_STRIP_TRAILING_WHITESPACE
            )
        endif()

        if(GIT_DESCRIBE_RES EQUAL 0 AND NOT "${GIT_DESCRIBE_OUT}" STREQUAL "")
            # Ensure leading 'v' for consistency
            if(NOT "${GIT_DESCRIBE_OUT}" MATCHES "^v")
                set(GIT_DESCRIBE_STR "v${GIT_DESCRIBE_OUT}")
            else()
                set(GIT_DESCRIBE_STR "${GIT_DESCRIBE_OUT}")
            endif()
        elseif(NOT "${GIT_COMMIT_SHA}" STREQUAL "unknown")
            # Repository has commits but no tags yet: check dirty state
            execute_process(
                COMMAND ${GIT_EXECUTABLE} status --porcelain
                WORKING_DIRECTORY "${SOURCE_DIR}"
                OUTPUT_VARIABLE GIT_STATUS_OUT
                RESULT_VARIABLE GIT_STATUS_RES
                ERROR_QUIET
                OUTPUT_STRIP_TRAILING_WHITESPACE
            )
            if(NOT "${GIT_STATUS_OUT}" STREQUAL "")
                set(GIT_DESCRIBE_STR "v0.1.0-0-g${GIT_COMMIT_SHA}-dirty")
            else()
                set(GIT_DESCRIBE_STR "v0.1.0-0-g${GIT_COMMIT_SHA}")
            endif()
        endif()
    endif()

    if(NOT "${GIT_DESCRIBE_STR}" STREQUAL "")
        # Extract base tag (vX.Y.Z) and trailing label (<commits>-g<sha>[-dirty])
        string(REGEX REPLACE "^(v[0-9\\.]+)-(.*)$" "\\1" GIT_RAW_TAG "${GIT_DESCRIBE_STR}")
        string(REGEX REPLACE "^v[0-9\\.]+-(.*)$" "\\1" GIT_DESCRIBE_TAIL "${GIT_DESCRIBE_STR}")

        # If commits since tag is 0, collapse "0-g<sha>" -> "" (or "-dirty" if dirty)
        if("${GIT_DESCRIBE_TAIL}" MATCHES "^0-g[0-9a-fA-F]+$")
            set(GIT_VERSION_LABEL "")
        elseif("${GIT_DESCRIBE_TAIL}" MATCHES "^0-g[0-9a-fA-F]+-dirty$")
            set(GIT_VERSION_LABEL "-dirty")
        else()
            set(GIT_VERSION_LABEL "-${GIT_DESCRIBE_TAIL}")
        endif()

        # Parse Major.Minor.Patch numbers
        if("${GIT_RAW_TAG}" MATCHES "^v([0-9]+)\\.([0-9]+)\\.([0-9]+)")
            set(RAW_MAJOR "${CMAKE_MATCH_1}")
            set(RAW_MINOR "${CMAKE_MATCH_2}")
            set(RAW_PATCH "${CMAKE_MATCH_3}")
        elseif("${GIT_RAW_TAG}" MATCHES "^v([0-9]+)\\.([0-9]+)")
            set(RAW_MAJOR "${CMAKE_MATCH_1}")
            set(RAW_MINOR "${CMAKE_MATCH_2}")
            set(RAW_PATCH "0")
        elseif("${GIT_RAW_TAG}" MATCHES "^v([0-9]+)")
            set(RAW_MAJOR "${CMAKE_MATCH_1}")
            set(RAW_MINOR "0")
            set(RAW_PATCH "0")
        else()
            set(RAW_MAJOR "0")
            set(RAW_MINOR "1")
            set(RAW_PATCH "0")
        endif()

        # Strip leading zeros (e.g. "01" -> "1") for CMake numeric VERSION compatibility
        string(REGEX REPLACE "^0+([0-9]+)$" "\\1" GIT_VER_MAJOR "${RAW_MAJOR}")
        string(REGEX REPLACE "^0+([0-9]+)$" "\\1" GIT_VER_MINOR "${RAW_MINOR}")
        string(REGEX REPLACE "^0+([0-9]+)$" "\\1" GIT_VER_PATCH "${RAW_PATCH}")
    endif()

    set(FULL_VERSION "${GIT_RAW_TAG}${GIT_VERSION_LABEL}")
    string(REGEX REPLACE "^v" "" SEMVER_VERSION "${FULL_VERSION}")

    set(${PREFIX}_VERSION        "${FULL_VERSION}"    PARENT_SCOPE)
    set(${PREFIX}_VERSION_SEMVER "${SEMVER_VERSION}"  PARENT_SCOPE)
    set(${PREFIX}_VER_MAJOR      "${GIT_VER_MAJOR}"   PARENT_SCOPE)
    set(${PREFIX}_VER_MINOR      "${GIT_VER_MINOR}"   PARENT_SCOPE)
    set(${PREFIX}_VER_PATCH      "${GIT_VER_PATCH}"   PARENT_SCOPE)
    set(${PREFIX}_VER_LABEL      "${GIT_VERSION_LABEL}" PARENT_SCOPE)
    set(${PREFIX}_GIT_COMMIT     "${GIT_COMMIT_SHA}"  PARENT_SCOPE)
    set(${PREFIX}_BUILD_TIME     "${BUILD_TIMESTAMP}" PARENT_SCOPE)

    message(STATUS "[GitVersion] ${PREFIX}_VERSION    = ${FULL_VERSION}")
    message(STATUS "[GitVersion] ${PREFIX}_GIT_COMMIT = ${GIT_COMMIT_SHA}")
    message(STATUS "[GitVersion] ${PREFIX}_BUILD_TIME = ${BUILD_TIMESTAMP}")
endfunction()

