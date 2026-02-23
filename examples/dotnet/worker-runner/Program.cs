using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using PipelogiqSDK.Abstractions;
using PipelogiqSDK.Api;
using PipelogiqSDK.Configuration;
using PipelogiqSDK.Contracts;
using PipelogiqSDK.Runner;
using PipelogiqSDK.StageHelper;

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
    WorkerName = "dotnet-example-worker",
    Environment = Environment.GetEnvironmentVariable("DOTNET_ENVIRONMENT") ?? "dev",
    Metadata = new Dictionary<string, string>
    {
        ["sample"] = "worker-runner",
        ["source"] = "examples/dotnet/worker-runner"
    }
};

using var host = Host.CreateDefaultBuilder(args)
    .ConfigureServices(services =>
    {
        services.AddPipelogiq(options);
        services.AddTransient<ValidateOrderHandler>();
        services.AddTransient<ChargeCardHandler>();
    })
    .Build();

var runner = host.Services.GetRequiredService<PipelineRunner>();
runner.RegisterHandler<ValidateOrderHandler>(nameof(ValidateOrderHandler));
runner.RegisterHandler<ChargeCardHandler>(nameof(ChargeCardHandler));

using var cts = new CancellationTokenSource();
Console.CancelKeyPress += (_, eventArgs) =>
{
    eventArgs.Cancel = true;
    cts.Cancel();
};

Console.WriteLine("Worker runner started. Press Ctrl+C to stop.");
await runner.StartAsync(cts.Token);

public record ValidateOrderInput(string OrderId, decimal Amount, string Currency, string CustomerEmail);
public record ChargeCardInput(string OrderId, string PaymentMethod);

public sealed class ValidateOrderHandler : IStageHandler<ValidateOrderInput>, IStageHandler
{
    public Task<IStageResult> ExecuteAsync(ValidateOrderInput input, IStageContext? context = null)
    {
        if (context is StageContext stageContext)
        {
            stageContext.Logger?.Info($"Validating order {input.OrderId} for {input.Amount} {input.Currency}");
        }

        context.AddItem("validationCheckedAt", DateTime.UtcNow);
        context.AddItem("validationResult", "ok");

        if (input.Amount <= 0)
        {
            return Task.FromResult<IStageResult>(StageResult.Error("Order amount must be greater than zero."));
        }

        return Task.FromResult<IStageResult>(StageResult.Success($"Order {input.OrderId} validated."));
    }

    public Task<IStageResult> ExecuteAsync(IStageContext? context = null)
    {
        return ExecuteAsync(new ValidateOrderInput("unknown", 0, "USD", "unknown@example.com"), context);
    }
}

public sealed class ChargeCardHandler : IStageHandler<ChargeCardInput>, IStageHandler
{
    public Task<IStageResult> ExecuteAsync(ChargeCardInput input, IStageContext? context = null)
    {
        if (context is StageContext stageContext)
        {
            stageContext.Logger?.Info($"Charging order {input.OrderId} using {input.PaymentMethod}");
        }

        context.AddItem("paymentProcessedAt", DateTime.UtcNow);
        context.AddItem("paymentProvider", "sandbox-gateway");

        return Task.FromResult<IStageResult>(StageResult.Success($"Payment processed for order {input.OrderId}."));
    }

    public Task<IStageResult> ExecuteAsync(IStageContext? context = null)
    {
        return ExecuteAsync(new ChargeCardInput("unknown", "card"), context);
    }
}
