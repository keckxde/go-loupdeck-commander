cmake_minimum_required(VERSION 3.22)

if(POLICY CMP0177)
    cmake_policy(SET CMP0177 NEW)
endif()

list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/cmake")

# --- Determine Version from Git Tag ---
include(GitVersion)
determine_git_version(LOUPEDECK_COMMANDER "${CMAKE_CURRENT_SOURCE_DIR}")

project(loupedeck-commander
    VERSION "${LOUPEDECK_COMMANDER_VER_MAJOR}.${LOUPEDECK_COMMANDER_VER_MINOR}.${LOUPEDECK_COMMANDER_VER_PATCH}"
    DESCRIPTION "Loupedeck Commander (Go) — Hardware Daemon, Emulator & Configuration Tool"
    LANGUAGES NONE
)

# Expose also under ${PROJECT_NAME}_* for convenience
set(${PROJECT_NAME}_VERSION "${LOUPEDECK_COMMANDER_VERSION}")
set(${PROJECT_NAME}_VERSION_SEMVER "${LOUPEDECK_COMMANDER_VERSION_SEMVER}")
set(${PROJECT_NAME}_VER_MAJOR "${LOUPEDECK_COMMANDER_VER_MAJOR}")
set(${PROJECT_NAME}_VER_MINOR "${LOUPEDECK_COMMANDER_VER_MINOR}")
set(${PROJECT_NAME}_VER_PATCH "${LOUPEDECK_COMMANDER_VER_PATCH}")
set(${PROJECT_NAME}_VER_LABEL "${LOUPEDECK_COMMANDER_VER_LABEL}")
set(${PROJECT_NAME}_GIT_COMMIT "${LOUPEDECK_COMMANDER_GIT_COMMIT}")
set(${PROJECT_NAME}_BUILD_TIME "${LOUPEDECK_COMMANDER_BUILD_TIME}")

set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_CURRENT_BINARY_DIR}/bin")
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY "${CMAKE_CURRENT_BINARY_DIR}/lib")

# --- Build Options & Cache Variables ---
option(GO_BUILD "Build the Go loupedeck-commander executable" ON)
option(ENABLE_TESTING "Enable Go unit tests via CTest" ON)
option(ENABLE_RACE_DETECTOR "Run Go tests with -race detector enabled" OFF)
option(GENERATE_INSTALLER "Configure CPack package generation (DEB/TGZ/ZIP)" ON)
option(SYNC_ROOT_BINARY "Also copy the built binary to the project root directory" ON)

set(BINARY_NAME "loupedeck-commander" CACHE STRING "Base name of the output binary")
set(CMD_PKG "./cmd/loupedeck-commander" CACHE STRING "Go main package path to build")
set(GO_TARGET_OS "" CACHE STRING "Target GOOS for cross-compilation (e.g. linux, windows)")
set(GO_TARGET_ARCH "" CACHE STRING "Target GOARCH for cross-compilation (e.g. amd64, arm64)")

# --- Locate Go Toolchain ---
find_program(GO_EXECUTABLE go REQUIRED DOC "Path to the Go toolchain executable")
execute_process(
    COMMAND ${GO_EXECUTABLE} version
    OUTPUT_VARIABLE GO_VERSION_OUTPUT
    OUTPUT_STRIP_TRAILING_WHITESPACE
)
message(STATUS "Found Go toolchain: ${GO_EXECUTABLE} (${GO_VERSION_OUTPUT})")

# Resolve effective target OS/arch and binary extension
if("${GO_TARGET_OS}" STREQUAL "windows" OR (WIN32 AND "${GO_TARGET_OS}" STREQUAL ""))
    set(BINARY_EXT ".exe")
else()
    set(BINARY_EXT "")
endif()

set(OUTPUT_BINARY_FILENAME "${BINARY_NAME}${BINARY_EXT}")
set(OUTPUT_BINARY_PATH "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/${OUTPUT_BINARY_FILENAME}")

# Configure Go linker flags (-ldflags) injecting Git tag version, commit, and build timestamp
set(GO_VERSION_LDFLAGS "-X main.Version=${LOUPEDECK_COMMANDER_VERSION} -X main.Commit=${LOUPEDECK_COMMANDER_GIT_COMMIT} -X main.BuildTime=${LOUPEDECK_COMMANDER_BUILD_TIME}")
if(CMAKE_BUILD_TYPE STREQUAL "Debug" OR CMAKE_BUILD_TYPE STREQUAL "RelWithDebInfo")
    set(GO_LDFLAGS "${GO_VERSION_LDFLAGS}")
    set(GO_GCFLAGS "all=-N -l")
else()
    set(GO_LDFLAGS "-s -w ${GO_VERSION_LDFLAGS}")
    set(GO_GCFLAGS "")
endif()

# Prepare environment variables for offline Go builds from ./vendor
set(GO_BUILD_ENV
    "CGO_ENABLED=0"
    "GOPROXY=off"
    "GOSUMDB=off"
)
if(NOT "${GO_TARGET_OS}" STREQUAL "")
    list(APPEND GO_BUILD_ENV "GOOS=${GO_TARGET_OS}")
endif()
if(NOT "${GO_TARGET_ARCH}" STREQUAL "")
    list(APPEND GO_BUILD_ENV "GOARCH=${GO_TARGET_ARCH}")
endif()

# Collect Go source files for accurate incremental rebuild tracking
file(GLOB_RECURSE GO_SOURCE_FILES
    CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/cmd/*.go"
    "${CMAKE_CURRENT_SOURCE_DIR}/pkg/*.go"
    "${CMAKE_CURRENT_SOURCE_DIR}/pkg/emulator/emulator-ui.html"
)

# Write a version stamp file that updates only when the git version or commit changes,
# ensuring the binary is automatically relinked whenever a git tag is added/moved.
set(VERSION_STAMP_FILE "${CMAKE_CURRENT_BINARY_DIR}/version.stamp")
file(CONFIGURE
    OUTPUT "${VERSION_STAMP_FILE}"
    CONTENT "VERSION=${LOUPEDECK_COMMANDER_VERSION}\nCOMMIT=${LOUPEDECK_COMMANDER_GIT_COMMIT}\nBUILD_TYPE=${CMAKE_BUILD_TYPE}\nGOOS=${GO_TARGET_OS}\nGOARCH=${GO_TARGET_ARCH}\n"
    @ONLY
)

if(GO_BUILD)
    file(MAKE_DIRECTORY "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}")

    if(SYNC_ROOT_BINARY)
        set(SYNC_ROOT_CMD
            COMMAND ${CMAKE_COMMAND} -E copy_if_different
            "${OUTPUT_BINARY_PATH}"
            "${CMAKE_CURRENT_SOURCE_DIR}/${OUTPUT_BINARY_FILENAME}"
        )
    else()
        set(SYNC_ROOT_CMD "")
    endif()

    if(NOT "${GO_GCFLAGS}" STREQUAL "")
        set(GO_GCFLAGS_ARGS "-gcflags=${GO_GCFLAGS}")
    else()
        set(GO_GCFLAGS_ARGS "")
    endif()

    add_custom_command(
        OUTPUT "${OUTPUT_BINARY_PATH}"
        DEPENDS
        "${VERSION_STAMP_FILE}"
        "${CMAKE_CURRENT_SOURCE_DIR}/go.mod"
        "${CMAKE_CURRENT_SOURCE_DIR}/go.sum"
        ${GO_SOURCE_FILES}
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}"
        COMMAND ${CMAKE_COMMAND} -E env ${GO_BUILD_ENV}
        ${GO_EXECUTABLE} build
        -mod=vendor
        -trimpath
        -buildvcs=false
        ${GO_GCFLAGS_ARGS}
        -ldflags "${GO_LDFLAGS}"
        -o "${OUTPUT_BINARY_PATH}"
        ${CMD_PKG}
        ${SYNC_ROOT_CMD}
        COMMENT "Building ${OUTPUT_BINARY_FILENAME} (version: ${LOUPEDECK_COMMANDER_VERSION})"
        VERBATIM
    )

    add_custom_target(${PROJECT_NAME}-bin ALL
        DEPENDS "${OUTPUT_BINARY_PATH}"
    )

    # Convenience cross-compile targets matching the Makefile
    add_custom_target(build-linux
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}"
        COMMAND ${CMAKE_COMMAND} -E env CGO_ENABLED=0 GOPROXY=off GOSUMDB=off GOOS=linux GOARCH=amd64
        ${GO_EXECUTABLE} build -mod=vendor -trimpath -buildvcs=false
        -ldflags "-s -w ${GO_VERSION_LDFLAGS}"
        -o "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/${BINARY_NAME}"
        ${CMD_PKG}
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/${BINARY_NAME}"
        "${CMAKE_CURRENT_SOURCE_DIR}/${BINARY_NAME}"
        COMMENT "Cross-compiling Linux amd64 binary (${LOUPEDECK_COMMANDER_VERSION})"
        VERBATIM
    )

    add_custom_target(build-windows
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        COMMAND ${CMAKE_COMMAND} -E make_directory "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}"
        COMMAND ${CMAKE_COMMAND} -E env CGO_ENABLED=0 GOPROXY=off GOSUMDB=off GOOS=windows GOARCH=amd64
        ${GO_EXECUTABLE} build -mod=vendor -trimpath -buildvcs=false
        -ldflags "-s -w ${GO_VERSION_LDFLAGS}"
        -o "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/${BINARY_NAME}.exe"
        ${CMD_PKG}
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/${BINARY_NAME}.exe"
        "${CMAKE_CURRENT_SOURCE_DIR}/${BINARY_NAME}.exe"
        COMMENT "Cross-compiling Windows amd64 binary (${LOUPEDECK_COMMANDER_VERSION})"
        VERBATIM
    )
endif()

# Online vendor refresh target
add_custom_target(vendor
    WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
    COMMAND ${CMAKE_COMMAND} -E env GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org
    ${GO_EXECUTABLE} mod tidy
    COMMAND ${CMAKE_COMMAND} -E env GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org
    ${GO_EXECUTABLE} mod vendor
    COMMAND ${GO_EXECUTABLE} mod verify
    COMMENT "Refreshing ./vendor from go.mod (requires network access)"
    VERBATIM
)

# --- Unit Testing via CTest ---
if(ENABLE_TESTING)
    enable_testing()

    set(GO_TEST_FLAGS -mod=vendor -v)
    if(ENABLE_RACE_DETECTOR)
        list(APPEND GO_TEST_FLAGS -race)
        set(GO_TEST_CGO "CGO_ENABLED=1")
    else()
        set(GO_TEST_CGO "CGO_ENABLED=0")
    endif()

    add_test(
        NAME ${PROJECT_NAME}-unit-tests
        COMMAND ${CMAKE_COMMAND} -E env ${GO_TEST_CGO} GOPROXY=off GOSUMDB=off
        ${GO_EXECUTABLE} test ${GO_TEST_FLAGS} ./...
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
    )

    if(GO_BUILD AND ("${GO_TARGET_OS}" STREQUAL "" OR "${GO_TARGET_OS}" STREQUAL "linux"))
        add_test(
            NAME ${PROJECT_NAME}-version-check
            COMMAND "${OUTPUT_BINARY_PATH}" --version
            WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        )
        set_tests_properties(${PROJECT_NAME}-version-check PROPERTIES
            PASS_REGULAR_EXPRESSION "${LOUPEDECK_COMMANDER_VERSION}"
        )
    endif()
endif()

# --- Installation & Packaging (CPack) ---
install(
    PROGRAMS "${OUTPUT_BINARY_PATH}"
    DESTINATION bin
    COMPONENT CORE
)

if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/README.md")
    install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/README.md" DESTINATION doc COMPONENT CORE)
endif()
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/LICENSE")
    install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/LICENSE" DESTINATION licenses COMPONENT CORE)
endif()
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/config")
    install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/config/" DESTINATION config COMPONENT CORE)
endif()
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/data")
    install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/data/" DESTINATION data COMPONENT CORE)
endif()

if(GENERATE_INSTALLER)
    set(CPACK_PACKAGE_NAME "${PROJECT_NAME}")
    set(CPACK_PACKAGE_VENDOR "KECKXDE")
    set(CPACK_PACKAGE_CONTACT "keckxde@gmail.com")
    set(CPACK_PACKAGE_DESCRIPTION_SUMMARY "${PROJECT_DESCRIPTION}" CACHE STRING "Package description summary")
    set(CPACK_DEBIAN_PACKAGE_DEPENDS "usbutils" CACHE STRING "Debian package dependencies")
    set(CPACK_PACKAGE_VERSION "${LOUPEDECK_COMMANDER_VERSION_SEMVER}")
    set(CPACK_PACKAGE_VERSION_MAJOR "${LOUPEDECK_COMMANDER_VER_MAJOR}")
    set(CPACK_PACKAGE_VERSION_MINOR "${LOUPEDECK_COMMANDER_VER_MINOR}")
    set(CPACK_PACKAGE_VERSION_PATCH "${LOUPEDECK_COMMANDER_VER_PATCH}")
    set(CPACK_VERBATIM_VARIABLES YES)

    if("${GO_TARGET_OS}" STREQUAL "windows" OR WIN32)
        set(CPACK_GENERATOR "ZIP")
        set(CPACK_SYSTEM_NAME "Windows-amd64")
        set(CPACK_PACKAGING_INSTALL_PREFIX "")
    else()
        set(CPACK_GENERATOR "DEB;TGZ")
        set(CPACK_DEBIAN_FILE_NAME DEB-DEFAULT)
        set(CPACK_DEBIAN_PACKAGE_MAINTAINER "KECKXDE")
        set(CPACK_DEBIAN_PACKAGE_SECTION "utils")
        set(CPACK_DEBIAN_PACKAGE_PRIORITY "optional")
        set(CPACK_DEBIAN_PACKAGE_SHLIBDEPS OFF)
        if("${GO_TARGET_ARCH}" STREQUAL "arm64")
            set(CPACK_DEBIAN_PACKAGE_ARCHITECTURE "arm64")
        else()
            set(CPACK_DEBIAN_PACKAGE_ARCHITECTURE "amd64")
        endif()
    endif()

    include(CPack)
endif()
