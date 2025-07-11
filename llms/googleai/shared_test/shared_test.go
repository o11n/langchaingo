// nolint
package shared_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/httputil"
	"github.com/tmc/langchaingo/internal/httprr"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/googleai/vertex"
)

func newGoogleAIClient(t *testing.T, opts ...googleai.Option) *googleai.GoogleAI {
	t.Helper()

	// Always check for recordings first - prefer recordings over environment variables
	if !hasExistingRecording(t) {
		t.Skip("No httprr recording available. Hint: Re-run tests with -httprecord=. to record new HTTP interactions")
	}

	// Temporarily unset Google API key environment variable to prevent bypass
	oldKey := os.Getenv("GOOGLE_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	t.Cleanup(func() {
		if oldKey != "" {
			os.Setenv("GOOGLE_API_KEY", oldKey)
		}
	})

	rr := httprr.OpenForTest(t, httputil.DefaultTransport)

	// Scrub API key for security in recordings
	rr.ScrubReq(func(req *http.Request) error {
		q := req.URL.Query()
		if q.Get("key") != "" {
			q.Set("key", "test-api-key")
			req.URL.RawQuery = q.Encode()
		}
		return nil
	})

	// Configure client with httprr and test credentials
	opts = append(opts,
		googleai.WithRest(),
		googleai.WithAPIKey("test-api-key"),
		googleai.WithHTTPClient(rr.Client()),
	)

	llm, err := googleai.New(context.Background(), opts...)
	require.NoError(t, err)
	return llm
}

func newVertexClient(t *testing.T, opts ...googleai.Option) *vertex.Vertex {
	t.Helper()

	// Always check for recordings first - prefer recordings over environment variables
	if !hasExistingRecording(t) {
		t.Skip("No httprr recording available. Hint: Re-run tests with -httprecord=. to record new HTTP interactions")
	}

	// Temporarily unset Google API key environment variable to prevent bypass
	oldKey := os.Getenv("GOOGLE_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	t.Cleanup(func() {
		if oldKey != "" {
			os.Setenv("GOOGLE_API_KEY", oldKey)
		}
	})

	rr := httprr.OpenForTest(t, httputil.DefaultTransport)

	// Configure client with httprr and test credentials
	opts = append(opts,
		googleai.WithHTTPClient(rr.Client()),
		googleai.WithCloudProject("test-project"),
		googleai.WithCloudLocation("us-central1"),
	)

	llm, err := vertex.New(context.Background(), opts...)
	require.NoError(t, err)
	return llm
}

// hasExistingRecording checks if a httprr recording exists for this test
func hasExistingRecording(t *testing.T) bool {
	testName := strings.ReplaceAll(t.Name(), "/", "_")
	testName = strings.ReplaceAll(testName, " ", "_")
	recordingPath := filepath.Join("testdata", testName+".httprr")
	_, err := os.Stat(recordingPath)
	return err == nil
}

// funcName obtains the name of the given function value, without a package
// prefix.
func funcName(f any) string {
	fullName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	parts := strings.Split(fullName, ".")
	return parts[len(parts)-1]
}

// testConfigs is a list of all test functions in this file to run with both
// client types, and their client configurations.
type testConfig struct {
	testFunc func(*testing.T, llms.Model)
	opts     []googleai.Option
}

var testConfigs = []testConfig{
	{testMultiContentText, nil},
	{testGenerateFromSinglePrompt, nil},
	{testMultiContentTextChatSequence, nil},
	{testMultiContentWithSystemMessage, nil},
	{testMultiContentImageLink, nil},
	{testMultiContentImageBinary, nil},
	{testEmbeddings, nil},
	{testCandidateCountSetting, nil},
	{testMaxTokensSetting, nil},
	{testTools, nil},
	{testToolsWithInterfaceRequired, nil},
	{
		testMultiContentText,
		[]googleai.Option{googleai.WithHarmThreshold(googleai.HarmBlockMediumAndAbove)},
	},
	{testWithStreaming, nil},
	{testWithHTTPClient, getHTTPTestClientOptions()},
}

func TestGoogleAIShared(t *testing.T) {
	for _, c := range testConfigs {
		t.Run(fmt.Sprintf("%s-googleai", funcName(c.testFunc)), func(t *testing.T) {
			llm := newGoogleAIClient(t, c.opts...)
			c.testFunc(t, llm)
		})
	}
}

func TestVertexShared(t *testing.T) {
	for _, c := range testConfigs {
		t.Run(fmt.Sprintf("%s-vertex", funcName(c.testFunc)), func(t *testing.T) {
			llm := newVertexClient(t, c.opts...)
			c.testFunc(t, llm)
		})
	}
}

func testMultiContentText(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	parts := []llms.ContentPart{
		llms.TextPart("I'm a pomeranian"),
		llms.TextPart("What kind of mammal am I?"),
	}
	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: parts,
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "(?i)dog|carnivo|canid|canine", c1.Content)
	assert.Contains(t, c1.GenerationInfo, "output_tokens")
	assert.NotZero(t, c1.GenerationInfo["output_tokens"])
}

func testMultiContentTextUsingTextParts(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := llms.TextParts(
		llms.ChatMessageTypeHuman,
		"I'm a pomeranian",
		"What kind of mammal am I?",
	)

	rsp, err := llm.GenerateContent(context.Background(), []llms.MessageContent{content})
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "(?i)dog|canid|canine", c1.Content)
}

func testGenerateFromSinglePrompt(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	prompt := "name all the planets in the solar system"
	rsp, err := llms.GenerateFromSinglePrompt(context.Background(), llm, prompt)
	require.NoError(t, err)

	assert.Regexp(t, "(?i)jupiter", rsp)
}

func testMultiContentTextChatSequence(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Name some countries")},
		},
		{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextPart("Spain and Lesotho")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Which if these is larger?")},
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content, llms.WithModel("gemini-1.5-flash"))
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "(?i)spain.*larger", c1.Content)
}

func testMultiContentWithSystemMessage(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart("You are a Spanish teacher; answer in Spanish")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Name the 5 most common fruits")},
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content, llms.WithModel("gemini-1.5-flash"))
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "(manzana|naranja)")
}

func testMultiContentImageLink(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	parts := []llms.ContentPart{
		llms.ImageURLPart(
			"https://github.com/tmc/langchaingo/blob/main/docs/static/img/parrot-icon.png?raw=true",
		),
		llms.TextPart("describe this image in detail"),
	}
	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: parts,
		},
	}

	rsp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithModel("gemini-pro-vision"),
	)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "parrot")
}

func testMultiContentImageBinary(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	b, err := os.ReadFile(filepath.Join("testdata", "parrot-icon.png"))
	if err != nil {
		t.Fatal(err)
	}

	parts := []llms.ContentPart{
		llms.BinaryPart("image/png", b),
		llms.TextPart("what does this image show? please use detail"),
	}
	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: parts,
		},
	}

	rsp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithModel("gemini-pro-vision"),
	)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "parrot")
}

func testEmbeddings(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	texts := []string{"foo", "parrot", "foo"}
	emb := llm.(embeddings.EmbedderClient)
	res, err := emb.CreateEmbedding(context.Background(), texts)
	require.NoError(t, err)

	assert.Equal(t, len(texts), len(res))
	assert.NotEmpty(t, res[0])
	assert.NotEmpty(t, res[1])
	assert.Equal(t, res[0], res[2])
}

func testCandidateCountSetting(t *testing.T, llm llms.Model) {
	t.Helper()

	parts := []llms.ContentPart{
		llms.TextPart("Name five countries in Africa"),
	}
	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: parts,
		},
	}

	{
		rsp, err := llm.GenerateContent(context.Background(), content,
			llms.WithCandidateCount(1), llms.WithTemperature(1))
		require.NoError(t, err)

		assert.Len(t, rsp.Choices, 1)
	}

	// TODO: test multiple candidates when the backend supports it
}

func testWithStreaming(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	content := llms.TextParts(
		llms.ChatMessageTypeHuman,
		"I'm a pomeranian",
		"Tell me more about my taxonomy",
	)

	var sb strings.Builder
	rsp, err := llm.GenerateContent(
		context.Background(),
		[]llms.MessageContent{content},
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			sb.Write(chunk)
			return nil
		}))

	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	checkMatch(t, c1.Content, "(dog|canid)")
	checkMatch(t, sb.String(), "(dog|canid)")
}

func testTools(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	availableTools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "getCurrentWeather",
				Description: "Get the current weather in a given location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "What is the weather like in Chicago?"),
	}
	resp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 := resp.Choices[0]

	// Update chat history with assistant's response, with its tool calls.
	assistantResp := llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
	}
	for _, tc := range c1.ToolCalls {
		assistantResp.Parts = append(assistantResp.Parts, tc)
	}
	content = append(content, assistantResp)

	// "Execute" tool calls by calling requested function
	for _, tc := range c1.ToolCalls {
		switch tc.FunctionCall.Name {
		case "getCurrentWeather":
			var args struct {
				Location string `json:"location"`
			}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(args.Location, "Chicago") {
				toolResponse := llms.MessageContent{
					Role: llms.ChatMessageTypeTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{
							Name:    tc.FunctionCall.Name,
							Content: "64 and sunny",
						},
					},
				}
				content = append(content, toolResponse)
			}
		default:
			t.Errorf("got unexpected function call: %v", tc.FunctionCall.Name)
		}
	}

	resp, err = llm.GenerateContent(context.Background(), content, llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 = resp.Choices[0]
	checkMatch(t, c1.Content, "(64 and sunny|64 degrees)")
	assert.Contains(t, resp.Choices[0].GenerationInfo, "output_tokens")
	assert.NotZero(t, resp.Choices[0].GenerationInfo["output_tokens"])
}

func testToolsWithInterfaceRequired(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	availableTools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "getCurrentWeather",
				Description: "Get the current weather in a given location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
					},
					// json.Unmarshal() may return []interface{} instead of []string
					"required": []interface{}{"location"},
				},
			},
		},
	}

	content := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "What is the weather like in Chicago?"),
	}
	resp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 := resp.Choices[0]
	assert.Contains(t, c1.GenerationInfo, "output_tokens")
	assert.NotZero(t, c1.GenerationInfo["output_tokens"])

	// Update chat history with assistant's response, with its tool calls.
	assistantResp := llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
	}
	for _, tc := range c1.ToolCalls {
		assistantResp.Parts = append(assistantResp.Parts, tc)
	}
	content = append(content, assistantResp)

	// "Execute" tool calls by calling requested function
	for _, tc := range c1.ToolCalls {
		switch tc.FunctionCall.Name {
		case "getCurrentWeather":
			var args struct {
				Location string `json:"location"`
			}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(args.Location, "Chicago") {
				toolResponse := llms.MessageContent{
					Role: llms.ChatMessageTypeTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{
							Name:    tc.FunctionCall.Name,
							Content: "64 and sunny",
						},
					},
				}
				content = append(content, toolResponse)
			}
		default:
			t.Errorf("got unexpected function call: %v", tc.FunctionCall.Name)
		}
	}

	resp, err = llm.GenerateContent(context.Background(), content, llms.WithTools(availableTools))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	c1 = resp.Choices[0]
	checkMatch(t, c1.Content, "(64 and sunny|64 degrees)")
	assert.Contains(t, resp.Choices[0].GenerationInfo, "output_tokens")
	assert.NotZero(t, resp.Choices[0].GenerationInfo["output_tokens"])
}

func testMaxTokensSetting(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	parts := []llms.ContentPart{
		llms.TextPart("I'm a pomeranian"),
		llms.TextPart("Describe my taxonomy, health and care"),
	}
	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: parts,
		},
	}

	// First, try this with a very low MaxTokens setting for such a query; expect
	// a stop reason that max of tokens was reached.
	{
		rsp, err := llm.GenerateContent(context.Background(), content,
			llms.WithMaxTokens(24))
		require.NoError(t, err)

		assert.NotEmpty(t, rsp.Choices)
		c1 := rsp.Choices[0]
		// TODO: Google genai models are returning "FinishReasonStop" instead of "MaxTokens".
		assert.Regexp(t, "(?i)(MaxTokens|FinishReasonStop)", c1.StopReason)
	}

	// Now, try it again with a much larger MaxTokens setting and expect to
	// finish successfully and generate a response.
	{
		rsp, err := llm.GenerateContent(context.Background(), content,
			llms.WithMaxTokens(2048))
		require.NoError(t, err)

		assert.NotEmpty(t, rsp.Choices)
		c1 := rsp.Choices[0]
		checkMatch(t, c1.StopReason, "stop")
		checkMatch(t, c1.Content, "(dog|breed|canid|canine)")
	}
}

func testWithHTTPClient(t *testing.T, llm llms.Model) {
	t.Helper()
	t.Parallel()

	resp, err := llm.GenerateContent(
		context.TODO(),
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "testing")},
	)
	require.NoError(t, err)
	require.EqualValues(t, "test-ok", resp.Choices[0].Content)
}

func getHTTPTestClientOptions() []googleai.Option {
	client := &http.Client{Transport: &testRequestInterceptor{}}
	return []googleai.Option{googleai.WithRest(), googleai.WithHTTPClient(client)}
}

type testRequestInterceptor struct{}

func (i *testRequestInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {
	defer req.Body.Close()
	content := `{
					"candidates": [{
						"content": {
							"parts": [{"text": "test-ok"}]
						},
						"finishReason": "STOP"
					}],
					"usageMetadata": {
						"promptTokenCount": 7,
						"candidatesTokenCount": 7,
						"totalTokenCount": 14
					}
				}`

	resp := &http.Response{
		StatusCode: http.StatusOK, Request: req,
		Body:   io.NopCloser(bytes.NewBufferString(content)),
		Header: http.Header{},
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func showJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// checkMatch is a testing helper that checks `got` for regexp matches vs.
// `wants`. Each of `wants` has to match.
func checkMatch(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		re, err := regexp.Compile("(?i:" + want + ")")
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(got) {
			t.Errorf("\ngot %q\nwanted to match %q", got, want)
		}
	}
}
