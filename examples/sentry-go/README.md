# Sentry Go demo app

This small web app uses the official `sentry-go` SDK. It sends an exception to a GoodSentry project when you press the button.

## Run it

1. Start GoodSentry and create a project. Copy that project's DSN.
2. From this directory, run:

   ```sh
   SENTRY_DSN='http://PUBLIC_KEY@localhost:8000/PROJECT_ID' go run .
   ```

   Replace the example URL with the DSN shown in your project. If GoodSentry runs at another address, use that address in the DSN.

3. Open [http://localhost:8081](http://localhost:8081) and select **Send a demo exception**.
4. Open the project's **Issues** page in GoodSentry. The event is tagged with the `demo` environment and `goodsentry-sentry-go-demo@1.0.0` release by default.

The SDK version is pinned to `sentry-go v0.50.0`, matching the version used by GoodSentry's compatibility checks. Set `SENTRY_ENVIRONMENT`, `SENTRY_RELEASE`, or `ADDR` to override the defaults.
