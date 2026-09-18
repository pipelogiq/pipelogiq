using PipelogiqSDK.Api;
using PipelogiqSDK.Configuration;
using PipelogiqSDK.Contracts;

var apiUrl = Environment.GetEnvironmentVariable("PIPELOGIQ_API_URL") ?? "http://localhost:8081";
var apiKey = Environment.GetEnvironmentVariable("PIPELOGIQ_API_KEY");

if (string.IsNullOrWhiteSpace(apiKey))
{
    Console.Error.WriteLine("Set PIPELOGIQ_API_KEY before running this sample.");
    return;
}

var options = new PipelogiqRunnerOptions
{
    ApiUrl = apiUrl,
    ApiKey = apiKey,
};

var client = new PipelogiqApiClient(options);

var bootstrapRequest = new WorkerBootstrapRequest
{
    WorkerName = "dotnet-lifecycle-sample",
    InstanceId = $"{Environment.MachineName}-{Environment.ProcessId}",
    WorkerVersion = "0.5.0",
    SdkVersion = typeof(PipelogiqApiClient).Assembly.GetName().Version?.ToString(3),
    Environment = Environment.GetEnvironmentVariable("DOTNET_ENVIRONMENT") ?? "dev",
    HostName = Environment.MachineName,
    Pid = Environment.ProcessId,
    SupportedHandlers = new List<string>
    {
        "ValidateOrderHandler",
        "ChargeCardHandler"
    },
    Capabilities = new Dictionary<string, bool>
    {
        ["batchAck"] = false,
        ["otel"] = false
    },
    Metadata = new Dictionary<string, string>
    {
        ["sample"] = "api-client-worker-lifecycle",
        ["source"] = "examples/dotnet/api-client-worker-lifecycle"
    }
};

WorkerBootstrapResponse bootstrap;
try
{
    bootstrap = await client.PostWorkerBootstrapAsync(bootstrapRequest);
}
catch (HttpRequestException ex)
{
    Console.Error.WriteLine($"Bootstrap failed (HTTP status: {ex.StatusCode?.ToString() ?? "unavailable"}).");
    Environment.ExitCode = 1;
    return;
}

if (string.IsNullOrWhiteSpace(bootstrap.WorkerId) || string.IsNullOrWhiteSpace(bootstrap.WorkerSessionToken))
{
    Console.Error.WriteLine("Bootstrap response is missing worker identity/session token.");
    return;
}

Console.WriteLine("Bootstrap OK:");
Console.WriteLine($"  WorkerId: {bootstrap.WorkerId}");
Console.WriteLine($"  AppId: {bootstrap.Application.AppId}");
Console.WriteLine($"  Broker: {bootstrap.MessageBroker.Type}");
Console.WriteLine($"  Prefetch: {bootstrap.MessageBroker.Prefetch}");

// The bootstrap response contains MessageBroker.ConnectionString for an actual
// worker runtime. Never log it or WorkerSessionToken: both contain credentials.

await client.PostWorkerHeartbeatAsync(
    bootstrap.WorkerSessionToken,
    new WorkerHeartbeatRequest
    {
        WorkerId = bootstrap.WorkerId,
        State = "ready",
        UptimeSec = 3,
        BrokerConnected = false, // This control-plane example never connects to RabbitMQ.
        InFlightJobs = 0,
        JobsProcessed = 0,
        JobsFailed = 0,
        QueueLag = 0,
        CpuPercent = 1.5,
        MemoryMb = 120,
        Message = "heartbeat from sample",
        Metadata = new Dictionary<string, object?>
        {
            ["mode"] = "sample",
            ["observability.traceLinkTemplate"] = bootstrap.Observability?.TraceLinkTemplate
        }
    });

await client.PostWorkerEventAsync(
    bootstrap.WorkerSessionToken,
    new WorkerEventRequest
    {
        WorkerId = bootstrap.WorkerId,
        Events = new List<WorkerEventItem>
        {
            new()
            {
                Timestamp = DateTime.UtcNow,
                Level = "INFO",
                EventType = "sample.event",
                Message = "Worker lifecycle sample emitted event",
                Details = new Dictionary<string, object?>
                {
                    ["example"] = true,
                    ["jobsProcessed"] = 0,
                    ["jobsFailed"] = 0
                }
            }
        }
    });

await client.PostWorkerShutdownAsync(
    bootstrap.WorkerSessionToken,
    new WorkerShutdownRequest
    {
        WorkerId = bootstrap.WorkerId,
        Reason = "Sample completed"
    });

Console.WriteLine("Lifecycle calls completed (bootstrap -> heartbeat -> event -> shutdown).");
