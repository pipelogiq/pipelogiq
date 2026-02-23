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
    WorkerVersion = "1.0.0",
    SdkVersion = "example",
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
        ["otel"] = true
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
    Console.Error.WriteLine("Bootstrap failed.");
    Console.Error.WriteLine(ex.Message);
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

try
{
    var rabbit = await client.GetRabbitMqConnectionAsync();
    Console.WriteLine($"  RabbitMQ connection from API: {rabbit.ConnectionString}");
}
catch (HttpRequestException ex)
{
    Console.WriteLine($"  RabbitMQ connection endpoint failed: {ex.Message}");
}

await client.PostWorkerHeartbeatAsync(
    bootstrap.WorkerSessionToken,
    new WorkerHeartbeatRequest
    {
        WorkerId = bootstrap.WorkerId,
        State = "ready",
        UptimeSec = 3,
        BrokerConnected = true,
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
        Type = "sample.event",
        Message = "Worker lifecycle sample emitted event",
        Metadata = new Dictionary<string, object?>
        {
            ["example"] = true,
            ["queuedAt"] = DateTimeOffset.UtcNow
        }
    });

await client.PostWorkerShutdownAsync(
    bootstrap.WorkerSessionToken,
    new WorkerShutdownRequest
    {
        WorkerId = bootstrap.WorkerId,
        State = "stopped",
        Message = "Sample completed",
        Metadata = new Dictionary<string, object?>
        {
            ["jobsProcessed"] = 0,
            ["jobsFailed"] = 0
        }
    });

Console.WriteLine("Lifecycle calls completed (bootstrap -> heartbeat -> event -> shutdown).");
