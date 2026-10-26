#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#include <sentry.h>

#if defined(__GNUC__) || defined(__clang__)
#define NOINLINE __attribute__((noinline))
#else
#define NOINLINE
#endif

static const char *required_environment(const char *name)
{
    const char *value = getenv(name);
    if (value == NULL || value[0] == '\0') {
        fprintf(stderr, "%s is required\n", name);
        exit(EXIT_FAILURE);
    }
    return value;
}

static NOINLINE void goodsentry_demo_crash(void)
{
    volatile uintptr_t address = 0;
    *(volatile int *)address = 42;
}

int main(void)
{
    const char *dsn = required_environment("SENTRY_DSN");
    const char *handler = required_environment("SENTRY_CRASHPAD_HANDLER");
    const char *database = required_environment("SENTRY_DATABASE_PATH");
    if (handler[0] != '/' || database[0] != '/') {
        fprintf(stderr, "SENTRY_CRASHPAD_HANDLER and SENTRY_DATABASE_PATH must be absolute paths\n");
        return EXIT_FAILURE;
    }

    sentry_options_t *options = sentry_options_new();
    if (options == NULL) {
        fprintf(stderr, "Could not allocate Sentry options\n");
        return EXIT_FAILURE;
    }
    sentry_options_set_dsn(options, dsn);
    sentry_options_set_release(options, "goodsentry-native-demo@1.0.0");
    sentry_options_set_environment(options, "demo");
    sentry_options_set_database_path(options, database);
    sentry_options_set_handler_path(options, handler);
    sentry_options_set_debug(options, 1);

    if (sentry_init(options) != 0) {
        fprintf(stderr, "Could not initialize sentry-native\n");
        return EXIT_FAILURE;
    }

    puts("Sending an intentional native crash to GoodSentry.");
    fflush(stdout);
    goodsentry_demo_crash();

    (void)sentry_close();
    return EXIT_SUCCESS;
}
