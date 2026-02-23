# .NET SDK Examples

These examples use the local SDK source at `../pipelogiq-sdk-net/src` via project references.

## Structure

- `pipeline-sender`: send pipelines/events/logs with `PipelineBuilder`, `EventBuilder`, `LogBuilder`, and `PipelineService`.
- `worker-runner`: run `PipelineRunner` with registered handlers (`RegisterHandler<T>`).
- `api-client-worker-lifecycle`: low-level worker lifecycle calls with `PipelogiqApiClient`.

## Prerequisites

- .NET 8 SDK
- Pipelogiq API running (external API on `http://localhost:8081` by default)
- Valid API key

## Environment Variables

```bash
export PIPELOGIQ_API_URL=http://localhost:8081
export PIPELOGIQ_API_KEY=<your-api-key>
```

## Run

```bash
cd examples/dotnet/pipeline-sender
DOTNET_CLI_UI_LANGUAGE=en dotnet run

cd ../worker-runner
DOTNET_CLI_UI_LANGUAGE=en dotnet run

cd ../api-client-worker-lifecycle
DOTNET_CLI_UI_LANGUAGE=en dotnet run
```

## Notes

- The worker sample runs until stopped (`Ctrl+C`).
- Handler names used in sender sample (`ValidateOrderHandler`, `ChargeCardHandler`) match the worker sample.
- If you want to consume the published NuGet package instead of local source, replace each `ProjectReference` with a `PackageReference` to `PipelogiqSDK`.
