package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rs/zerolog"
)

var (
	log zerolog.Logger
)

func endpoint(region, tld string, local bool) string {
	if local {
		return "http://localhost:5600/mcp"
	}

	return fmt.Sprintf("https://%s.mcp.konghq.%s/mcp", region, tld)
}

func main() {
	var (
		loggingFile io.Writer
		err         error
	)

	loggingFile, err = os.Create("/tmp/konnect-mcp.logs")
	if err != nil {
		loggingFile = io.Discard
	}

	log = zerolog.New(loggingFile)

	pat := strings.TrimSpace(os.Getenv("KONNECT_MCP_TOKEN"))
	region := "us"
	tld := "com"
	local := false
	if pat == "" {
		log.Err(errors.New("PAT token not set in env")).Msg("unable to load PAT token")
		os.Exit(3)
		return
	}

	flag.StringVar(&region, "region", region, "Kong Konnect Personal Access Token")
	flag.StringVar(&tld, "tld", region, "TLD to use when rendering the MCP endpoint")
	flag.BoolVar(&local, "local", local, "Testing flag")

	flag.Parse()

	mcpEndpoint := endpoint(region, tld, local)
	ctx := context.Background()

	log = log.With().
		Str("endpoint", mcpEndpoint).
		Str("tld", tld).
		Str("region", region).
		Str("pat", pat).
		Bool("is_local", local).
		Logger()

	httpTransport, err := transport.NewStreamableHTTP(mcpEndpoint, transport.WithHTTPHeaders(map[string]string{
		"Authorization": "Bearer " + pat,
	}))
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create a HTTP transport")
	}

	client := client.NewClient(httpTransport)
	// Set up notification handler
	client.OnNotification(func(notification mcp.JSONRPCNotification) {
		fmt.Printf("Received notification: %s\n", notification.Method)
	})

	log.Info().Msg("attempting to connect to mcp endpoint")
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "MCP-Go Simple Client Example",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	serverInfo, err := client.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize client")
	}
	if serverInfo.Capabilities.Tools == nil {
		log.Fatal().Err(errors.New("mcp server doesn't support tools")).Msg("failed to initialize client")
	}

	log.Info().Msg("connected to mcp endpoint")

	toolsRequest := mcp.ListToolsRequest{}
	toolsResult, err := client.ListTools(ctx, toolsRequest)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to list tools")
	}

	log.Info().Msg("listing tools")

	runServer(ctx, client, toolsResult.Tools)
}

func runServer(_ context.Context, session *client.Client, tools []mcp.Tool) {
	srv := server.NewMCPServer("konnect-mcp-stdio", "1.0.0")

	for _, tool := range tools {
		log.Info().
			Str("tool_name", tool.Name).
			Msg("added tool")

		srv.AddTool(tool, func(ctx context.Context, ctr mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			log.Info().
				Str("tool_name", ctr.Params.Name).
				Any("arguments", ctr.Params.Arguments).
				Any("meta", ctr.Params.Meta).
				Msg("tool called")
			return session.CallTool(ctx, ctr)
		})
	}

	if err := server.ServeStdio(srv); err != nil {
		log.Fatal().Err(err).Msg("unable run stdio server")
	}
}
