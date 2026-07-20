package config

// DefaultModels is the model catalog compiled into Oolong, used when the
// config file does not provide its own [[models]] catalog.
// Rates per the providers' published API pricing.
var DefaultModels = []Model{
	{ID: "gpt-5.6-luna", Provider: "openai", Description: "For cost-sensitive workloads", InputRate: 1.00, OutputRate: 6.00, ContextWindow: 400_000},
	{ID: "gpt-5.6-terra", Provider: "openai", Description: "Balances intelligence and cost", InputRate: 2.50, OutputRate: 15.00, ContextWindow: 400_000},
	{ID: "gpt-5.6-sol", Provider: "openai", Description: "For complex professional work", InputRate: 5.00, OutputRate: 30.00, ContextWindow: 400_000},
	{ID: "claude-sonnet-5", Provider: "anthropic", Description: "Balanced speed, cost, and intelligence", InputRate: 2.00, OutputRate: 10.00, ContextWindow: 1_000_000},
	{ID: "claude-opus-4-8", Provider: "anthropic", Description: "Powerful model for complex work", InputRate: 5.00, OutputRate: 25.00, ContextWindow: 1_000_000},
	{ID: "claude-fable-5", Provider: "anthropic", Description: "Flagship model for the hardest problems", InputRate: 10.00, OutputRate: 50.00, ContextWindow: 1_000_000},
	{ID: "gemini-3.5-flash", Provider: "google", Description: "Fast, capable everyday model", InputRate: 1.50, OutputRate: 9.00, ContextWindow: 1_000_000},
	{ID: "gemini-3.1-flash-lite", Provider: "google", Description: "Lowest-latency budget model", InputRate: 0.25, OutputRate: 1.50, ContextWindow: 1_000_000},
}
