package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
)

func endpoint(region, tld string, local bool) string {
	if local {
		return "http://localhost:5600/mcp"
	}

	return fmt.Sprintf("https://%s.mcp.konghq.%s/mcp", region, tld)
}

type patRoundTripper struct {
	pat string
}

func (p *patRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Insomnia/12.0.0")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.pat))

	ev := log.Info().Str("method", req.Method)

	for k := range req.Header {
		ev = ev.Str(k, req.Header.Get(k))
	}

	ev.Msg("init request")

	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		log.Info().Str("mcp_body", string(body)).Msg("mcp contact")
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		log.Info().Int("mcp_status_code", resp.StatusCode).Msg("got response")
	} else {
		log.Error().Err(err).Msg("error response")
	}

	return resp, err
}

var (
	log zerolog.Logger
)

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

	pat := strings.TrimSpace(os.Getenv("ZED_KONNECT_TOKEN"))
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

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "konnect-mcp-stdio",
		Title:   "Konnect MCP CLI Server relaying stdio client calls to a HTTP MCP implementation",
		Version: "0.0.1",
	}, nil)

	httpClient := http.Client{
		Transport: &patRoundTripper{pat},
	}

	log.Info().Msg("attempting to connect to mcp endpoint")

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   mcpEndpoint,
		HTTPClient: &httpClient,
	}, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to connect to remote")
	}
	defer session.Close()

	log.Info().Msg("connected to mcp endpoint")
	log.Info().Msg("listing tools")

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to list tools")
	}

	log.Info().Msg("listing tools successful")

	runServer(ctx, session, toolsResult.Tools)
}

func runServer(ctx context.Context, session *mcp.ClientSession, tools []*mcp.Tool) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "konnect-mcp-stdio",
		Version: "1.0.0",
	}, nil)

	for _, tool := range tools {
		log.Info().
			Str("tool_name", tool.Name).
			Msg("added tool")

		server.AddTool(tool, func(ctx context.Context, ctr *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			log.Info().
				Str("tool_name", ctr.Params.Name).
				Any("arguments", ctr.Params.Arguments).
				Any("meta", ctr.Params.Meta).
				Msg("tool called")
			return session.CallTool(ctx, &mcp.CallToolParams{
				Name:      ctr.Params.Name,
				Arguments: ctr.Params.Arguments,
				Meta:      ctr.Params.Meta,
			})
		})
	}

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal().Err(err).Msg("unable run stdio server")
	}
}
