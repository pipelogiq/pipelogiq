using Microsoft.Extensions.Logging;
using PipelogiqSDK.Api;
using PipelogiqSDK.Builders;
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

var correlationId = Guid.NewGuid().ToString("N");

var pipelineBuilder = PipelineBuilder.Create("orders.checkout", options)
    .WithAction(
        stageName: "validate-order",
        stageHandlerName: "ValidateOrderHandler",
        input: new
        {
            OrderId = correlationId,
            Amount = 199.99m,
            Currency = "USD",
            CustomerEmail = "customer@example.com"
        },
        options: new StageOptions
        {
            MaxRetries = 3,
            RetryInterval = 5,
            TimeOut = 30,
            NotifyOnFailure = true,
        })
    .WithAction(
        stageName: "charge-card",
        stageHandlerName: "ChargeCardHandler",
        input: new
        {
            OrderId = correlationId,
            PaymentMethod = "card"
        })
    .AddLabel("service", "pipeline-sender-example")
    .AddLabel("env", Environment.GetEnvironmentVariable("ASPNETCORE_ENVIRONMENT") ?? "dev")
    .AddContextItem("correlationId", correlationId)
    .AddContextItem("source", "examples/dotnet/pipeline-sender");

await PipelineService.StartPipelineAsync(pipelineBuilder);
Console.WriteLine("Pipeline submitted.");

var eventBuilder = EventBuilder.Create("user.created", "UserCreatedHandler", options)
    .AddLabel("event", "user.created")
    .AddContextItem("userId", 123456)
    .AddContextItem("email", "new-user@example.com");

await PipelineService.StartEventAsync(eventBuilder);
Console.WriteLine("Event pipeline submitted.");

var logBuilder = LogBuilder.Create(LogLevel.Information, "pipeline sender example completed", options)
    .AddLabel("sample", "pipeline-sender")
    .AddLabel("correlationId", correlationId);

await PipelineService.SendLogAsync(logBuilder);
Console.WriteLine("Log entry submitted.");
