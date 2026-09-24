// Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors
// Modified 2026 by BareMetal
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

type Configuration struct {
	Server *ServerConfig
	// Networks is every network the process should connect to. Always has
	// at least one entry; on the single-network path it holds Server alone.
	Networks []*ServerConfig
	Bot      *BotConfig
	Model    *ModelConfig
	Session  *SessionConfig
	API      *APIConfig
}

type ServerConfig struct {
	// Name identifies the network.
	Name string
	// ResponsePrefix overrides Bot.ResponsePrefix on this network. A pointer, so an explicit ""
	// clears the prefix instead of inheriting it.
	ResponsePrefix *string
	Nick           string
	Server         string
	Port           int
	Channel        string
	ChannelKey     string
	SSL            bool
	TLSInsecure    bool
	SASLNick       string
	SASLPass       string
	ServerPass     string
}

type BotConfig struct {
	Admins             []string
	Verbose            bool
	LogLevel           string // debug, info, warn, error
	LogFormat          string // text, json
	Addressed          bool
	Trigger            string
	ResponsePrefix     string
	Prompt             string
	Greeting           string
	OpWatcher          bool
	OpWatcherTemplate  string
	Tools              []string
	AdminTools         []string
	ShowThinkingAction bool
	ShowToolActions    bool
	URLWatcher         bool
	URLWatcherSilent   bool
	Sandbox            bool
	IgnorePrivate      bool
	// Flood protection: more than FloodMessages from one nick within
	// FloodWindow auto-ignores them for FloodTimeout. 0 messages disables it.
	FloodMessages int
	FloodWindow   time.Duration
	FloodTimeout  time.Duration

	// ScreenNicks are screened by a classifier before the model sees them; a refused
	// message never enters the session.
	ScreenNicks   []string
	ScreenRefusal string

	FilterNicks []string

	// PromptFloor prepends the operator's non-negotiable rules to any prompt a
	// user sets with +prompt. Off only for testing what a persona does unguarded.
	PromptFloor bool
	// DataDir holds runtime state: memories, reminders, ignores, overrides.
	DataDir string
	// MaxConcurrent is how many requests may run at once, across all networks.
	MaxConcurrent int
	// PluginLib is put on PYTHONPATH so tools can import metald_tools.
	PluginLib     string
	CommandPrefix string

	// Every model-facing prompt is overridable; defaults live in prompts.go.
	FloorPrompt        string
	GatekeeperPreamble string
	GatekeeperPolicy   string
	ClassifyPreamble   string
	ReplyScreenPolicy  string
	MemoryPolicy       string
	MemoryFrame        string
}

type ModelConfig struct {
	Model          string
	MaxTokens      int
	Temperature    float32
	TopP           float32
	ThinkingEffort string // off, low, medium, high
	Stream         bool   // true = streaming (default), false = non-streaming
}

type SessionConfig struct {
	ChunkMax   int
	MaxContext int
	TTL        time.Duration
}

type APIConfig struct {
	Timeout      time.Duration
	OpenAIKey    string
	OpenAIURL    string
	AnthropicKey string
	GeminiKey    string
	OllamaURL    string
	OllamaKey    string
}

// YamlSource implements cli.ValueSource for a map loaded from YAML
type YamlSource struct {
	data map[string]any
	key  string
}

func (y *YamlSource) Lookup() (string, bool) {
	if v, ok := y.data[y.key]; ok {
		// Handle slices by joining with comma
		if slice, ok := v.([]any); ok {
			var strs []string
			for _, item := range slice {
				strs = append(strs, fmt.Sprintf("%v", item))
			}
			return strings.Join(strs, ","), true
		}
		return fmt.Sprintf("%v", v), true
	}
	return "", false
}

func (y *YamlSource) String() string   { return "yaml" }
func (y *YamlSource) GoString() string { return "yaml" }

func GetFlags() []cli.Flag {
	// Pre-parse config path
	configPath := getConfigPath()
	var configData map[string]any
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := yaml.Unmarshal(data, &configData); err != nil {
				slog.Error("config_parse_failed", "path", configPath, "error", err.Error())
				os.Exit(1)
			}
			vars, err := ToolEnv(configData["env"])
			if err != nil {
				slog.Error("config_env_invalid", "path", configPath, "error", err.Error())
				os.Exit(1)
			}
			if len(vars) > 0 {
				set, fromEnv := ExportToolEnv(vars)
				slog.Info("tool_env_loaded", "from_config", len(set), "kept_from_environment", strings.Join(fromEnv, ","))
			}
			delete(configData, "env")
		} else {
			fmt.Fprintf(os.Stderr, "Warning: failed to read config file %s: %v\n", configPath, err)
		}
	}

	// Helper to create sources: EnvVar > YAML > Default
	src := func(key string, env ...string) cli.ValueSourceChain {
		chain := cli.ValueSourceChain{}
		for _, e := range env {
			chain.Chain = append(chain.Chain, cli.EnvVar(e))
		}
		if configData != nil {
			chain.Chain = append(chain.Chain, &YamlSource{data: configData, key: key})
		}
		return chain
	}

	return []cli.Flag{
		// Config file
		&cli.StringFlag{Name: "config", Aliases: []string{"b"}, Usage: "use the named configuration file", Sources: cli.EnvVars("METALD_CONFIG")},

		// IRC Client Configuration
		&cli.StringFlag{Name: "nick", Aliases: []string{"n"}, Value: "metald", Usage: "bot's nickname on the irc server", Sources: src("nick", "METALD_NICK")},
		&cli.StringFlag{Name: "server", Aliases: []string{"s"}, Value: "localhost", Usage: "irc server address", Sources: src("server", "METALD_SERVER")},
		&cli.BoolFlag{Name: "tls", Aliases: []string{"e"}, Usage: "enable TLS for the IRC connection", Sources: src("tls", "METALD_TLS")},
		&cli.BoolFlag{Name: "tlsinsecure", Usage: "skip TLS certificate verification", Sources: src("tlsinsecure", "METALD_TLSINSECURE")},
		&cli.IntFlag{Name: "port", Aliases: []string{"p"}, Value: 6667, Usage: "irc server port", Sources: src("port", "METALD_PORT")},
		&cli.StringFlag{Name: "channel", Aliases: []string{"c"}, Usage: "irc channel to join", Sources: src("channel", "METALD_CHANNEL")},
		&cli.StringFlag{Name: "channelkey", Usage: "channel key (password) for joining", Sources: src("channelkey", "METALD_CHANNELKEY")},
		&cli.StringFlag{Name: "saslnick", Usage: "nick used for SASL", Sources: src("saslnick", "METALD_SASLNICK")},
		&cli.StringFlag{Name: "saslpass", Usage: "password for SASL plain", Sources: src("saslpass", "METALD_SASLPASS")},
		&cli.StringFlag{Name: "serverpass", Usage: "server password (sent via PASS before NICK/USER)", Sources: src("serverpass", "METALD_SERVERPASS")},

		// Bot Configuration
		&cli.StringSliceFlag{Name: "admins", Aliases: []string{"A"}, Usage: "comma-separated list of allowed hostmasks to administrate the bot", Sources: src("admins", "METALD_ADMINS")},
		&cli.BoolFlag{Name: "verbose", Aliases: []string{"V"}, Usage: "enable verbose logging (shortcut for --loglevel=debug)", Sources: src("verbose", "METALD_VERBOSE")},
		&cli.StringFlag{Name: "loglevel", Value: "info", Usage: "log level: debug, info, warn, error", Sources: src("loglevel", "METALD_LOGLEVEL")},
		&cli.StringFlag{Name: "logformat", Value: "text", Usage: "log format: text (colorized), json", Sources: src("logformat", "METALD_LOGFORMAT")},

		// API Configuration
		&cli.StringFlag{Name: "openaikey", Usage: "OpenAI API key", Sources: src("openaikey", "METALD_OPENAIKEY")},
		&cli.StringFlag{Name: "openaiurl", Usage: "OpenAI API URL (for custom endpoints)", Sources: src("openaiurl", "METALD_OPENAIURL")},
		&cli.StringFlag{Name: "anthropickey", Usage: "Anthropic API key", Sources: src("anthropickey", "METALD_ANTHROPICKEY")},
		&cli.StringFlag{Name: "geminikey", Usage: "Google Gemini API key", Sources: src("geminikey", "METALD_GEMINIKEY")},
		&cli.StringFlag{Name: "ollamaurl", Value: "http://localhost:11434", Usage: "Ollama API URL", Sources: src("ollamaurl", "METALD_OLLAMAURL")},
		&cli.StringFlag{Name: "ollamakey", Usage: "Ollama API key (Bearer token for authentication)", Sources: src("ollamakey", "METALD_OLLAMAKEY")},
		&cli.IntFlag{Name: "maxtokens", Value: 16384, Usage: "maximum number of tokens to generate", Sources: src("maxtokens", "METALD_MAXTOKENS")},
		&cli.StringFlag{Name: "model", Value: "ollama/llama3.2", Usage: "model to be used for responses", Sources: src("model", "METALD_MODEL")},
		&cli.DurationFlag{Name: "apitimeout", Aliases: []string{"t"}, Value: time.Minute * 5, Usage: "timeout for each completion request", Sources: src("apitimeout", "METALD_APITIMEOUT")},
		&cli.FloatFlag{Name: "temperature", Value: 0.7, Usage: "temperature for the completion", Sources: src("temperature", "METALD_TEMPERATURE")},
		&cli.FloatFlag{Name: "top_p", Value: 1.0, Usage: "top P value for the completion", Sources: src("top_p", "METALD_TOP_P")},
		&cli.StringFlag{Name: "thinkingeffort", Value: "off", Usage: "thinking effort level: off, low, medium, high", Sources: src("thinkingeffort", "METALD_THINKINGEFFORT")},
		&cli.BoolFlag{Name: "stream", Value: true, Usage: "enable streaming responses", Sources: src("stream", "METALD_STREAM")},
		&cli.StringSliceFlag{Name: "tool", Usage: "tools to load (shell scripts, MCP server JSON files, or native tools like irc__op)", Sources: src("tool", "METALD_TOOL")},
		&cli.StringSliceFlag{Name: "admintools", Usage: "namespaced tool name(s) restricted to admins (e.g. 'websearch__web_search')", Sources: src("admintools", "METALD_ADMINTOOLS")},
		&cli.BoolFlag{Name: "showthinkingaction", Value: true, Usage: "show '[thinking]' IRC action when bot is reasoning", Sources: src("showthinkingaction", "METALD_SHOWTHINKINGACTION")},
		&cli.BoolFlag{Name: "showtoolactions", Value: true, Usage: "show '[calling toolname]' IRC actions when executing tools", Sources: src("showtoolactions", "METALD_SHOWTOOLACTIONS")},
		&cli.BoolFlag{Name: "urlwatcher", Usage: "enable passive URL watching and analysis", Sources: src("urlwatcher", "METALD_URLWATCHER")},
		&cli.BoolFlag{Name: "urlwatchersilent", Usage: "run URL watcher without sending a reply in chat; response is discarded", Sources: src("urlwatchersilent", "METALD_URLWATCHERSILENT")},
		&cli.BoolFlag{Name: "sandbox", Usage: "run shell/bash/MCP tools inside a platform sandbox (macOS sandbox-exec, Linux bubblewrap)", Sources: src("sandbox", "METALD_SANDBOX")},
		&cli.BoolFlag{Name: "ignoreprivate", Usage: "ignore direct/private messages entirely (no response, no commands)", Sources: src("ignoreprivate", "METALD_IGNOREPRIVATE")},
		&cli.IntFlag{Name: "floodmessages", Value: 5, Usage: "messages from one nick within floodwindow that trigger an auto-timeout (0 disables)", Sources: src("floodmessages", "METALD_FLOODMESSAGES")},
		&cli.StringSliceFlag{Name: "screennicks", Usage: "nicks whose messages are screened by a classifier before answering (empty disables)", Sources: src("screennicks", "METALD_SCREENNICKS")},
		&cli.StringFlag{Name: "screenrefusal", Value: "no.", Usage: "what to say when a screened message is refused", Sources: src("screenrefusal", "METALD_SCREENREFUSAL")},
		&cli.StringSliceFlag{Name: "filternicks", Usage: "nicks whose conversations also get the bot's OUTGOING replies screened before posting (empty disables)", Sources: src("filternicks", "METALD_FILTERNICKS")},
		&cli.BoolFlag{Name: "promptfloor", Value: true, Usage: "prepend the operator's rules to any user-set +prompt persona (disable only for testing)", Sources: src("promptfloor", "METALD_PROMPTFLOOR")},
		// No defaults: the shipped text is in examples/chatbot.yml and the bot
		// refuses to start if any of these is empty.
		&cli.IntFlag{Name: "maxconcurrent", Value: 3, Usage: "requests handled at once across all networks (1 = strictly one at a time)", Sources: src("maxconcurrent", "METALD_MAXCONCURRENT")},
		&cli.StringFlag{Name: "commandprefix", Value: "+", Usage: "what starts a command, e.g. + or ! (punctuation only)", Sources: src("commandprefix", "METALD_COMMANDPREFIX")},
		&cli.StringFlag{Name: "pluginlib", Value: "plugins/lib", Usage: "shared Python library for tools, added to PYTHONPATH", Sources: src("pluginlib", "METALD_PLUGINLIB")},
		&cli.StringFlag{Name: "datadir", Value: ".", Usage: "directory for runtime state (memories, reminders, ignores, overrides)", Sources: src("datadir", "METALD_DATADIR")},
		&cli.StringFlag{Name: "floorprompt", Usage: "text prepended to a user-set +prompt persona when promptfloor is on (required)", Sources: src("floorprompt", "METALD_FLOORPROMPT")},
		&cli.StringFlag{Name: "gatekeeperpreamble", Usage: "system preamble for the inbound message classifier (required)", Sources: src("gatekeeperpreamble", "METALD_GATEKEEPERPREAMBLE")},
		&cli.StringFlag{Name: "gatekeeperpolicy", Usage: "policy the inbound message classifier enforces for screened nicks (required)", Sources: src("gatekeeperpolicy", "METALD_GATEKEEPERPOLICY")},
		&cli.StringFlag{Name: "classifypreamble", Usage: "system preamble shared by the tool and memory classifiers (required)", Sources: src("classifypreamble", "METALD_CLASSIFYPREAMBLE")},
		&cli.StringFlag{Name: "replyscreenpolicy", Usage: "policy the outbound reply classifier enforces for filtered nicks (required)", Sources: src("replyscreenpolicy", "METALD_REPLYSCREENPOLICY")},
		&cli.StringFlag{Name: "memoryframe", Usage: "text introducing what the bot remembers about the speaker; {nick} is replaced (required)", Sources: src("memoryframe", "METALD_MEMORYFRAME")},
		&cli.StringFlag{Name: "memorypolicy", Usage: "policy checked before a fact is written to memory (required)", Sources: src("memorypolicy", "METALD_MEMORYPOLICY")},
		&cli.DurationFlag{Name: "floodwindow", Value: 30 * time.Second, Usage: "sliding window for flood detection", Sources: src("floodwindow", "METALD_FLOODWINDOW")},
		&cli.DurationFlag{Name: "floodtimeout", Value: 5 * time.Minute, Usage: "how long a flooding nick is auto-ignored", Sources: src("floodtimeout", "METALD_FLOODTIMEOUT")},

		// Timeouts and Behavior
		&cli.BoolFlag{Name: "addressed", Aliases: []string{"a"}, Value: true, Usage: "require bot be addressed by nick for response", Sources: src("addressed", "METALD_ADDRESSED")},
		&cli.StringFlag{Name: "trigger", Usage: "word/phrase that activates the bot instead of its nick (default: bot's nick)", Sources: src("trigger", "METALD_TRIGGER")},
		&cli.StringFlag{Name: "responseprefix", Usage: "prefix prepended to every response line (e.g. '[metalai]')", Sources: src("responseprefix", "METALD_RESPONSEPREFIX")},
		&cli.DurationFlag{Name: "sessionduration", Aliases: []string{"S"}, Value: time.Minute * 10, Usage: "message context will be cleared after it is unused for this duration", Sources: src("sessionduration", "METALD_SESSIONDURATION")},
		&cli.IntFlag{Name: "maxcontext", Value: 0, Usage: "maximum token count for session history (0 = unlimited)", Sources: src("maxcontext", "METALD_MAXCONTEXT")},
		&cli.IntFlag{Name: "chunkmax", Aliases: []string{"m"}, Value: 350, Usage: "maximum number of characters to send as a single message", Sources: src("chunkmax", "METALD_CHUNKMAX")},

		// Personality / Prompting
		&cli.StringFlag{Name: "greeting", Value: "hello.", Usage: "prompt to be used when the bot joins the channel", Sources: src("greeting", "METALD_GREETING")},
		&cli.BoolFlag{Name: "opwatcher", Usage: "enable +o watcher to trigger LLM on being opped", Sources: src("opwatcher", "METALD_OPWATCHER")},
		&cli.StringFlag{Name: "opwatchertemplate", Value: "you were just %s by %s", Usage: "prompt template: first %s=action (opped/deopped), second %s=nick", Sources: src("opwatchertemplate", "METALD_OPWATCHERTEMPLATE")},
		&cli.StringFlag{Name: "prompt", Value: "you are a helpful chatbot. do not use caps. do not use emoji.", Usage: "initial system prompt", Sources: src("prompt", "METALD_PROMPT")},
	}
}

func getConfigPath() string {
	// Check env first
	if v := os.Getenv("METALD_CONFIG"); v != "" {
		return v
	}
	// Check args
	for i, arg := range os.Args {
		if arg == "--config" || arg == "-b" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
		// Handle -b=... if needed, though standard is space
	}
	return ""
}

func (c *Configuration) PrintConfig() {
	mask := func(key string) string {
		if key == "" || len(key) <= 3 {
			return key
		}
		return strings.Repeat("*", len(key)-3) + key[len(key)-3:]
	}

	fields := []struct{ name, value string }{
		{"nick", c.Server.Nick},
		{"server", c.Server.Server},
		{"port", fmt.Sprintf("%d", c.Server.Port)},
		{"channel", c.Server.Channel},
		{"channelkey", mask(c.Server.ChannelKey)},
		{"tls", fmt.Sprintf("%t", c.Server.SSL)},
		{"tlsinsecure", fmt.Sprintf("%t", c.Server.TLSInsecure)},
		{"saslnick", c.Server.SASLNick},
		{"saslpass", mask(c.Server.SASLPass)},
		{"serverpass", mask(c.Server.ServerPass)},
		{"admins", fmt.Sprintf("%v", c.Bot.Admins)},
		{"verbose", fmt.Sprintf("%t", c.Bot.Verbose)},
		{"addressed", fmt.Sprintf("%t", c.Bot.Addressed)},
		{"trigger", c.Bot.Trigger},
		{"responseprefix", c.Bot.ResponsePrefix},
		{"chunkmax", fmt.Sprintf("%d", c.Session.ChunkMax)},
		{"clienttimeout", c.API.Timeout.String()},
		{"maxcontext", fmt.Sprintf("%d", c.Session.MaxContext)},
		{"maxtokens", fmt.Sprintf("%d", c.Model.MaxTokens)},
		{"tool", fmt.Sprintf("%v", c.Bot.Tools)},
		{"admintools", fmt.Sprintf("%v", c.Bot.AdminTools)},
		{"showthinkingaction", fmt.Sprintf("%t", c.Bot.ShowThinkingAction)},
		{"showtoolactions", fmt.Sprintf("%t", c.Bot.ShowToolActions)},
		{"urlwatcher", fmt.Sprintf("%t", c.Bot.URLWatcher)},
		{"urlwatchersilent", fmt.Sprintf("%t", c.Bot.URLWatcherSilent)},
		{"sandbox", fmt.Sprintf("%t", c.Bot.Sandbox)},
		{"ignoreprivate", fmt.Sprintf("%t", c.Bot.IgnorePrivate)},
		{"sessionduration", c.Session.TTL.String()},
		{"openaikey", mask(c.API.OpenAIKey)},
		{"anthropickey", mask(c.API.AnthropicKey)},
		{"geminikey", mask(c.API.GeminiKey)},
		{"openaiurl", c.API.OpenAIURL},
		{"ollamaurl", c.API.OllamaURL},
		{"model", c.Model.Model},
		{"temperature", fmt.Sprintf("%f", c.Model.Temperature)},
		{"topp", fmt.Sprintf("%f", c.Model.TopP)},
		{"thinkingeffort", c.Model.ThinkingEffort},
		{"stream", fmt.Sprintf("%t", c.Model.Stream)},
		{"prompt", c.Bot.Prompt},
		{"greeting", c.Bot.Greeting},
		{"opwatcher", fmt.Sprintf("%t", c.Bot.OpWatcher)},
		{"opwatchertemplate", c.Bot.OpWatcherTemplate},
	}

	for _, f := range fields {
		fmt.Printf("%s: %s\n", f.name, f.value)
	}
}

// EffectiveResponsePrefix is the prefix for replies on this network: the
// network's own override when it has one, otherwise the shared bot setting.
func (c *Configuration) EffectiveResponsePrefix() string {
	if c.Server != nil && c.Server.ResponsePrefix != nil {
		return *c.Server.ResponsePrefix
	}
	return c.Bot.ResponsePrefix
}

func NewConfiguration(c *cli.Command) *Configuration {
	if c.IsSet("config") {
		slog.Info("config_loaded", "path", c.String("config"))
	}

	config := &Configuration{
		Server: &ServerConfig{
			Nick:        c.String("nick"),
			Server:      c.String("server"),
			Port:        c.Int("port"),
			Channel:     c.String("channel"),
			ChannelKey:  c.String("channelkey"),
			SSL:         c.Bool("tls"),
			TLSInsecure: c.Bool("tlsinsecure"),
			SASLNick:    c.String("saslnick"),
			SASLPass:    c.String("saslpass"),
			ServerPass:  c.String("serverpass"),
		},
		Bot: &BotConfig{
			Admins:             c.StringSlice("admins"),
			Verbose:            c.Bool("verbose"),
			LogLevel:           c.String("loglevel"),
			LogFormat:          c.String("logformat"),
			Addressed:          c.Bool("addressed"),
			Trigger:            c.String("trigger"),
			ResponsePrefix:     c.String("responseprefix"),
			Prompt:             c.String("prompt"),
			Greeting:           c.String("greeting"),
			OpWatcher:          c.Bool("opwatcher"),
			OpWatcherTemplate:  c.String("opwatchertemplate"),
			Tools:              c.StringSlice("tool"),
			AdminTools:         c.StringSlice("admintools"),
			ShowThinkingAction: c.Bool("showthinkingaction"),
			ShowToolActions:    c.Bool("showtoolactions"),
			URLWatcher:         c.Bool("urlwatcher"),
			URLWatcherSilent:   c.Bool("urlwatchersilent"),
			Sandbox:            c.Bool("sandbox"),
			IgnorePrivate:      c.Bool("ignoreprivate"),
			FloodMessages:      c.Int("floodmessages"),
			ScreenNicks:        c.StringSlice("screennicks"),
			ScreenRefusal:      c.String("screenrefusal"),
			FilterNicks:        c.StringSlice("filternicks"),
			PromptFloor:        c.Bool("promptfloor"),
			DataDir:            c.String("datadir"),
			MaxConcurrent:      int(c.Int("maxconcurrent")),
			PluginLib:          c.String("pluginlib"),
			CommandPrefix:      c.String("commandprefix"),
			FloorPrompt:        c.String("floorprompt"),
			GatekeeperPreamble: c.String("gatekeeperpreamble"),
			GatekeeperPolicy:   c.String("gatekeeperpolicy"),
			ClassifyPreamble:   c.String("classifypreamble"),
			ReplyScreenPolicy:  c.String("replyscreenpolicy"),
			MemoryPolicy:       c.String("memorypolicy"),
			MemoryFrame:        c.String("memoryframe"),
			FloodWindow:        c.Duration("floodwindow"),
			FloodTimeout:       c.Duration("floodtimeout"),
		},
		Model: &ModelConfig{
			Model:          c.String("model"),
			MaxTokens:      c.Int("maxtokens"),
			Temperature:    float32(c.Float("temperature")),
			TopP:           float32(c.Float("top_p")),
			ThinkingEffort: c.String("thinkingeffort"),
			Stream:         c.Bool("stream"),
		},

		Session: &SessionConfig{
			ChunkMax:   c.Int("chunkmax"),
			MaxContext: c.Int("maxcontext"),
			TTL:        c.Duration("sessionduration"),
		},

		API: &APIConfig{
			Timeout:      c.Duration("apitimeout"),
			OpenAIKey:    c.String("openaikey"),
			OpenAIURL:    c.String("openaiurl"),
			AnthropicKey: c.String("anthropickey"),
			GeminiKey:    c.String("geminikey"),
			OllamaURL:    c.String("ollamaurl"),
			OllamaKey:    c.String("ollamakey"),
		},
	}

	// Every model-facing prompt must be configured. Starting with an empty
	// classifier policy would silently screen nothing, so refuse instead.
	if missing := MissingPrompts(config.Bot); len(missing) > 0 {
		slog.Error("prompts_missing",
			"keys", strings.Join(missing, ", "),
			"hint", "copy them from examples/chatbot.yml")
		os.Exit(1)
	}

	nets, err := parseNetworks(getConfigPath(), config.Server)
	if err != nil {
		slog.Error("networks_config_invalid", "error", err.Error())
		os.Exit(1)
	}
	if len(nets) == 0 {
		config.Networks = []*ServerConfig{config.Server}
	} else {
		config.Networks = nets
		// The first network becomes the process default, so anything still
		// reading cfg.Server (startup logging, reminders) behaves sanely.
		config.Server = nets[0]
		slog.Info("networks_configured", "count", len(nets))
	}

	return config
}
