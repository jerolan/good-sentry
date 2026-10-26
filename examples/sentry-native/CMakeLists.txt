cmake_minimum_required(VERSION 3.24)
project(goodsentry_native_demo LANGUAGES C CXX)

if(NOT CMAKE_SYSTEM_NAME STREQUAL "Linux")
  message(FATAL_ERROR "This demo currently targets Linux with Crashpad.")
endif()

include(FetchContent)

set(SENTRY_BACKEND "crashpad" CACHE STRING "" FORCE)
set(SENTRY_TRANSPORT "curl" CACHE STRING "" FORCE)
set(SENTRY_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(SENTRY_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(SENTRY_BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
set(SENTRY_ENABLE_INSTALL OFF CACHE BOOL "" FORCE)

FetchContent_Declare(
  sentry_native
  GIT_REPOSITORY https://github.com/getsentry/sentry-native.git
  GIT_TAG 0.17.4
  GIT_SUBMODULES_RECURSE TRUE
)
FetchContent_MakeAvailable(sentry_native)

add_executable(goodsentry-native-demo main.c)
target_link_libraries(goodsentry-native-demo PRIVATE sentry::sentry)
target_compile_options(goodsentry-native-demo PRIVATE -O0 -g -fno-omit-frame-pointer)
target_link_options(goodsentry-native-demo PRIVATE -Wl,--build-id=sha1)
set_target_properties(goodsentry-native-demo PROPERTIES C_STANDARD 11 C_STANDARD_REQUIRED YES)

if(NOT TARGET crashpad_handler)
  message(FATAL_ERROR "The pinned Sentry Native build did not provide crashpad_handler.")
endif()

add_custom_command(
  TARGET goodsentry-native-demo POST_BUILD
  COMMAND ${CMAKE_COMMAND} -E copy_if_different
    "$<TARGET_FILE:crashpad_handler>"
    "$<TARGET_FILE_DIR:goodsentry-native-demo>/crashpad_handler"
  VERBATIM
)
add_dependencies(goodsentry-native-demo crashpad_handler)
