# Sentry Native SDK demo

This Linux demo uses the official `sentry-native` 0.17.4 SDK with its Crashpad backend. It intentionally crashes inside `goodsentry_demo_crash` so you can confirm that GoodSentry receives and symbolicates a real native event.

## Build

Install CMake, GCC, a C++17 compiler, libcurl and zlib. Then build the demo; CMake fetches the pinned SDK and its source submodules:

```sh
cmake -S examples/sentry-native -B /tmp/goodsentry-native-demo-build -DCMAKE_BUILD_TYPE=Debug
cmake --build /tmp/goodsentry-native-demo-build --parallel
```

The build keeps DWARF information and copies the matching `crashpad_handler` beside the demo executable.

## Send a crash to GoodSentry

1. Start GoodSentry, create a project, and copy its DSN. Create an API token that has `project:releases` access to that project.
2. Set these values for the project and GoodSentry instance you started:

   ```sh
   export GOODSENTRY_URL='http://localhost:8000'
   export GOODSENTRY_ORGANIZATION='your-organization-slug'
   export GOODSENTRY_PROJECT='your-project-slug'
   export GOODSENTRY_RELEASES_TOKEN='your-project-releases-token'
   export SENTRY_DSN='http://PUBLIC_KEY@localhost:8000/PROJECT_ID'
   export SENTRY_CRASHPAD_HANDLER="$(realpath /tmp/goodsentry-native-demo-build/crashpad_handler)"
   export SENTRY_DATABASE_PATH="$(realpath -m /tmp/goodsentry-native-demo-data/sdk-cache)"
   ```

   Use the DSN and URL shown by your GoodSentry instance if it runs at a different address or under a path prefix.
3. Upload the symbols from the exact executable that will crash:

   ```sh
   curl --fail-with-body \
     -H "Authorization: Bearer ${GOODSENTRY_RELEASES_TOKEN}" \
     -F 'type=elf' \
     -F 'cpu_name=x86_64' \
     -F 'object_name=goodsentry-native-demo' \
     -F 'file=@/tmp/goodsentry-native-demo-build/goodsentry-native-demo' \
     "${GOODSENTRY_URL}/api/0/projects/${GOODSENTRY_ORGANIZATION}/${GOODSENTRY_PROJECT}/files/dsyms/"
   ```

4. Trigger the intentional crash:

   ```sh
   /tmp/goodsentry-native-demo-build/goodsentry-native-demo
   ```

Open the project's Issues page. The native event should show `goodsentry_demo_crash` in `main.c` with a source line. Remove the demo's temporary SDK data directory when you are done. GoodSentry stores the parsed event and symbols, not the uploaded minidump.

The demo sends only to the DSN you provide. It does not connect to Sentry or another hosted service.
