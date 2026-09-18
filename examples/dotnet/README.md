# .NET SDK Examples

These examples target Pipelogiq server `0.5.0` and .NET SDK `0.5.0`.
They use the sibling checkout at `../pipelogiq-sdk-net/src/Pipelogiq.Sdk` via project references.

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
- The worker handles all three names used by the sender: `ValidateOrderHandler`, `ChargeCardHandler`, and `UserCreatedHandler`. The payment stage is a simulated operation.
- The sender uses `AddLabel`; deprecated `AddKeyword` aliases are unnecessary for new code.
- The lifecycle sample sends `WorkerEventRequest.Events` entries and shuts down with `WorkerShutdownRequest.Reason`. It exercises control-plane calls only and does not connect to RabbitMQ.
- Broker configuration arrives in the bootstrap response. Connection strings and worker session tokens contain credentials and are never printed by these samples.
- If you want to consume the published NuGet package instead of local source, replace each `ProjectReference` with a `PackageReference` to `PipelogiqSDK` with `Version="0.5.0"` once that package is published.
