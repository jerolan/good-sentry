using System;
using System.Threading.Tasks;
using Sentry;

class Program
{
    static async Task Main()
    {
        using var sdk = SentrySdk.Init(options => {
            options.Dsn = Environment.GetEnvironmentVariable("SENTRY_DSN");
            options.Debug = true;
        });
        try { ThrowProbe(); }
        catch (Exception error) { SentrySdk.CaptureException(error); }
        await SentrySdk.FlushAsync(TimeSpan.FromSeconds(10));
    }

    static void ThrowProbe() => throw new InvalidOperationException("portable-sdk-probe");
}
