package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"spaish/internal/ai"
	"spaish/internal/app"
	"spaish/internal/config"
)

// handleInitCommand runs the first-run setup wizard: pick a provider, configure
// it, write the config file, and test the connection.
func handleInitCommand(_ []string) {
	in := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  spaiSH setup")
	fmt.Println("  ─────────────")
	fmt.Println("  Choose your AI provider:")
	fmt.Println("    1) Anthropic (Claude)      — recommended, needs an API key")
	fmt.Println("    2) OpenAI-compatible API   — any OpenAI-style endpoint + key")
	fmt.Println("    3) Ollama (local models)   — runs on your machine, no API key")
	fmt.Println()

	choice := ask(in, "Provider [1]", "1")

	cfg := &config.Config{}
	var testErr error

	switch choice {
	case "2":
		cfg.Provider.Kind = "openai"
		cfg.Provider.Endpoint = ask(in, "Endpoint (include /v1)", "https://api.openai.com/v1")
		cfg.Provider.APIKeyEnv = ask(in, "API key environment variable", "OPENAI_API_KEY")
		cfg.Provider.Model = ask(in, "Model", "gpt-4o")
		testErr = testCloud(ai.NewOpenAIProvider(cfg.Provider.Endpoint, os.Getenv(cfg.Provider.APIKeyEnv), cfg.Provider.Model, ai.RetryConfig{}), cfg.Provider.APIKeyEnv)

	case "3":
		cfg.Routing.PreferLocal = true
		cfg.Local.OllamaEndpoint = ask(in, "Ollama endpoint", "http://localhost:11434")
		cfg.Local.LocalModel = ask(in, "Model", "qwen2.5-coder:7b")
		p := ai.NewLocalProvider(cfg.Local.OllamaEndpoint, cfg.Local.LocalModel, ai.RetryConfig{})
		if !p.Available() {
			testErr = fmt.Errorf("Ollama not reachable at %s — start it with `ollama serve` and pull the model with `spai llm pull %s`", cfg.Local.OllamaEndpoint, cfg.Local.LocalModel)
		} else {
			testErr = testCloud(p, "")
		}

	default: // Anthropic
		cfg.Provider.Kind = "anthropic"
		cfg.Provider.APIKeyEnv = ask(in, "API key environment variable", "ANTHROPIC_API_KEY")
		cfg.Provider.Model = ask(in, "Model", ai.DefaultAnthropicModel)
		key := os.Getenv(cfg.Provider.APIKeyEnv)
		if key == "" {
			testErr = fmt.Errorf("%s is not set in your environment — export it, e.g.\n    export %s=sk-ant-...", cfg.Provider.APIKeyEnv, cfg.Provider.APIKeyEnv)
		} else {
			testErr = testCloud(ai.NewAnthropicProvider(key, cfg.Provider.Model, ai.RetryConfig{}), cfg.Provider.APIKeyEnv)
		}
	}

	path := app.ConfigPath()
	if err := config.Save(path, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to write config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n✓ wrote %s\n", path)

	if testErr != nil {
		fmt.Printf("⚠  %v\n", testErr)
		fmt.Println("  Config saved anyway — fix the above and run `spai init` again to re-test.")
		return
	}
	fmt.Println("✓ connection test succeeded")
	fmt.Println("\nYou're set. Try:  spai \"what's using port 8080?\"   or just  spai")
}

// ask prompts for a value, returning def when the user presses Enter.
func ask(r *bufio.Reader, label, def string) string {
	fmt.Printf("  %s: ", label)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// testCloud sends a tiny completion to verify the provider works.
func testCloud(p ai.Provider, keyEnv string) error {
	if keyEnv != "" && os.Getenv(keyEnv) == "" {
		return fmt.Errorf("%s is not set", keyEnv)
	}
	fmt.Print("  testing connection... ")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := ai.CompleteText(ctx, p, "", []ai.Message{{Role: "user", Content: "Reply with the single word: ok"}}); err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Println("ok")
	return nil
}
