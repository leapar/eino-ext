/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package ark implements chat model for ark runtime.
package ark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	autils "github.com/volcengine/volcengine-go-sdk/service/arkruntime/utils"

	"github.com/cloudwego/eino/callbacks"
	fmodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var _ fmodel.ToolCallingChatModel = (*ChatModel)(nil)

var (
	// all default values are from github.com/volcengine/volcengine-go-sdk/service/arkruntime/config.go
	defaultBaseURL    = "https://ark.cn-beijing.volces.com/api/v3"
	defaultRegion     = "cn-beijing"
	defaultRetryTimes = 2
	defaultTimeout    = 10 * time.Minute
)

var (
	ErrEmptyResponse = errors.New("empty response received from model")
)

type ChatModelConfig struct {
	// Timeout specifies the maximum duration to wait for API responses
	// If HTTPClient is set, Timeout will not be used.
	// Optional. Default: 10 minutes
	Timeout *time.Duration `json:"timeout"`

	// HTTPClient specifies the client to send HTTP requests.
	// If HTTPClient is set, Timeout will not be used.
	// Optional. Default &http.Client{Timeout: Timeout}
	HTTPClient *http.Client `json:"http_client"`

	// RetryTimes specifies the number of retry attempts for failed API calls
	// Optional. Default: 2
	RetryTimes *int `json:"retry_times"`

	// BaseURL specifies the base URL for Ark service
	// Optional. Default: "https://ark.cn-beijing.volces.com/api/v3"
	BaseURL string `json:"base_url"`
	// Region specifies the region where Ark service is located
	// Optional. Default: "cn-beijing"
	Region string `json:"region"`

	// The following three fields are about authentication - either APIKey or AccessKey/SecretKey pair is required
	// For authentication details, see: https://www.volcengine.com/docs/82379/1298459
	// APIKey takes precedence if both are provided
	APIKey    string `json:"api_key"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`

	// The following fields correspond to Ark's chat completion API parameters
	// Ref: https://www.volcengine.com/docs/82379/1298454

	// Model specifies the ID of endpoint on ark platform
	// Required
	Model string `json:"model"`

	// MaxTokens limits the maximum number of tokens that can be generated in the chat completion and the range of values is [0, 4096]
	// Optional. Default: 4096
	MaxTokens *int `json:"max_tokens,omitempty"`

	// Temperature specifies what sampling temperature to use
	// Generally recommend altering this or TopP but not both
	// Range: 0.0 to 1.0. Higher values make output more random
	// Optional. Default: 1.0
	Temperature *float32 `json:"temperature,omitempty"`

	// TopP controls diversity via nucleus sampling
	// Generally recommend altering this or Temperature but not both
	// Range: 0.0 to 1.0. Lower values make output more focused
	// Optional. Default: 0.7
	TopP *float32 `json:"top_p,omitempty"`

	// Stop sequences where the API will stop generating further tokens
	// Optional. Example: []string{"\n", "User:"}
	Stop []string `json:"stop,omitempty"`

	// FrequencyPenalty prevents repetition by penalizing tokens based on frequency
	// Range: -2.0 to 2.0. Positive values decrease likelihood of repetition
	// Optional. Default: 0
	FrequencyPenalty *float32 `json:"frequency_penalty,omitempty"`

	// LogitBias modifies likelihood of specific tokens appearing in completion
	// Optional. Map token IDs to bias values from -100 to 100
	LogitBias map[string]int `json:"logit_bias,omitempty"`

	// PresencePenalty prevents repetition by penalizing tokens based on presence
	// Range: -2.0 to 2.0. Positive values increase likelihood of new topics
	// Optional. Default: 0
	PresencePenalty *float32 `json:"presence_penalty,omitempty"`

	// CustomHeader the http header passed to model when requesting model
	CustomHeader map[string]string `json:"custom_header"`
}

func buildClient(config *ChatModelConfig) *arkruntime.Client {
	if len(config.BaseURL) == 0 {
		config.BaseURL = defaultBaseURL
	}
	if len(config.Region) == 0 {
		config.Region = defaultRegion
	}
	if config.Timeout == nil {
		config.Timeout = &defaultTimeout
	}
	if config.RetryTimes == nil {
		config.RetryTimes = &defaultRetryTimes
	}

	opts := []arkruntime.ConfigOption{
		arkruntime.WithRetryTimes(*config.RetryTimes),
		arkruntime.WithBaseUrl(config.BaseURL),
		arkruntime.WithRegion(config.Region),
		arkruntime.WithTimeout(*config.Timeout),
	}
	if config.HTTPClient != nil {
		opts = append(opts, arkruntime.WithHTTPClient(config.HTTPClient))
	}

	if len(config.APIKey) > 0 {
		return arkruntime.NewClientWithApiKey(config.APIKey, opts...)
	}

	return arkruntime.NewClientWithAkSk(config.AccessKey, config.SecretKey, opts...)
}

func NewChatModel(_ context.Context, config *ChatModelConfig) (*ChatModel, error) {
	if config == nil {
		config = &ChatModelConfig{}
	}
	client := buildClient(config)

	return &ChatModel{
		config: config,
		client: client,
	}, nil
}

type ChatModel struct {
	config *ChatModelConfig
	client *arkruntime.Client

	tools    []tool
	rawTools []*schema.ToolInfo
}

type CacheInfo struct {
	// ContextID specifies the id of prefix that can be used with WithPrefixCache option
	ContextID string
	// Usage specifies the token usage of prefix
	Usage schema.TokenUsage
}

// CreatePrefixCache creates a prefix context on the server side that will be automatically included
// in subsequent model calls without needing to resend these messages each time.
// This improves efficiency by reducing token usage and request size.
//
// Parameters:
//   - ctx: The context for the request
//   - prefix: Initial messages to be cached as prefix context
//   - ttl: Time-to-live in seconds for the cached prefix, default: 86400
//
// Returns:
//   - info: Information about the created prefix cache, including the context ID and token usage
//   - err: Any error encountered during the operation
//
// ref: https://www.volcengine.com/docs/82379/1396490#_1-%E5%88%9B%E5%BB%BA%E5%89%8D%E7%BC%80%E7%BC%93%E5%AD%98
func (cm *ChatModel) CreatePrefixCache(ctx context.Context, prefix []*schema.Message, ttl int) (info *CacheInfo, err error) {
	req := model.CreateContextRequest{
		Model:    cm.config.Model,
		Mode:     model.ContextModeCommonPrefix,
		Messages: make([]*model.ChatCompletionMessage, 0, len(prefix)),
		TTL:      nil,
	}
	for _, msg := range prefix {
		content, err := toArkContent(msg.Content, msg.MultiContent)
		if err != nil {
			return nil, fmt.Errorf("create prefix fail, convert message fail: %w", err)
		}

		req.Messages = append(req.Messages, &model.ChatCompletionMessage{
			Content:    content,
			Role:       string(msg.Role),
			ToolCallID: msg.ToolCallID,
			ToolCalls:  toArkToolCalls(msg.ToolCalls),
		})
	}
	if ttl > 0 {
		req.TTL = &ttl
	}

	resp, err := cm.client.CreateContext(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create prefix fail: %w", err)
	}
	return &CacheInfo{
		ContextID: resp.ID,
		Usage: schema.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

func (cm *ChatModel) Generate(ctx context.Context, in []*schema.Message, opts ...fmodel.Option) (
	outMsg *schema.Message, err error) {

	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	options := fmodel.GetCommonOptions(&fmodel.Options{
		Temperature: cm.config.Temperature,
		MaxTokens:   cm.config.MaxTokens,
		Model:       &cm.config.Model,
		TopP:        cm.config.TopP,
		Stop:        cm.config.Stop,
		Tools:       nil,
	}, opts...)

	arkOpts := fmodel.GetImplSpecificOptions(&arkOptions{customHeaders: cm.config.CustomHeader}, opts...)

	req, err := cm.genRequest(in, options)
	if err != nil {
		return nil, err
	}

	reqConf := &fmodel.Config{
		Model:       req.Model,
		MaxTokens:   dereferenceOrZero(req.MaxTokens),
		Temperature: dereferenceOrZero(req.Temperature),
		TopP:        dereferenceOrZero(req.TopP),
		Stop:        req.Stop,
	}

	tools := cm.rawTools
	if options.Tools != nil {
		tools = options.Tools
	}

	ctx = callbacks.OnStart(ctx, &fmodel.CallbackInput{
		Messages: in,
		Tools:    tools, // join tool info from call options
		Config:   reqConf,
	})

	var resp model.ChatCompletionResponse
	if arkOpts.contextID != nil {
		resp, err = cm.client.CreateContextChatCompletion(ctx, *convCompletionRequest(req, *arkOpts.contextID), arkruntime.WithCustomHeaders(arkOpts.customHeaders))
	} else {
		resp, err = cm.client.CreateChatCompletion(ctx, *req, arkruntime.WithCustomHeaders(arkOpts.customHeaders))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create chat completion: %w", err)
	}

	outMsg, err = cm.resolveChatResponse(resp)
	if err != nil {
		return nil, err
	}

	callbacks.OnEnd(ctx, &fmodel.CallbackOutput{
		Message:    outMsg,
		Config:     reqConf,
		TokenUsage: toModelCallbackUsage(outMsg.ResponseMeta),
	})

	return outMsg, nil
}

func (cm *ChatModel) Stream(ctx context.Context, in []*schema.Message, opts ...fmodel.Option) (
	outStream *schema.StreamReader[*schema.Message], err error) {

	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	options := fmodel.GetCommonOptions(&fmodel.Options{
		Temperature: cm.config.Temperature,
		MaxTokens:   cm.config.MaxTokens,
		Model:       &cm.config.Model,
		TopP:        cm.config.TopP,
		Stop:        cm.config.Stop,
		Tools:       nil,
	}, opts...)

	arkOpts := fmodel.GetImplSpecificOptions(&arkOptions{customHeaders: cm.config.CustomHeader}, opts...)

	req, err := cm.genRequest(in, options)
	if err != nil {
		return nil, err
	}

	req.Stream = ptrOf(true)
	req.StreamOptions = &model.StreamOptions{IncludeUsage: true}

	reqConf := &fmodel.Config{
		Model:       req.Model,
		MaxTokens:   dereferenceOrZero(req.MaxTokens),
		Temperature: dereferenceOrZero(req.Temperature),
		TopP:        dereferenceOrZero(req.TopP),
		Stop:        req.Stop,
	}

	tools := cm.rawTools
	if options.Tools != nil {
		tools = options.Tools
	}

	ctx = callbacks.OnStart(ctx, &fmodel.CallbackInput{
		Messages: in,
		Tools:    tools,
		Config:   reqConf,
	})

	var stream *autils.ChatCompletionStreamReader
	if arkOpts.contextID != nil {
		stream, err = cm.client.CreateContextChatCompletionStream(ctx, *convCompletionRequest(req, *arkOpts.contextID), arkruntime.WithCustomHeaders(arkOpts.customHeaders))
	} else {
		stream, err = cm.client.CreateChatCompletionStream(ctx, *req, arkruntime.WithCustomHeaders(arkOpts.customHeaders))
	}
	if err != nil {
		return nil, err
	}

	sr, sw := schema.Pipe[*fmodel.CallbackOutput](1)
	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				_ = sw.Send(nil, newPanicErr(panicErr, debug.Stack()))
			}

			sw.Close()
			_ = closeArkStreamReader(stream) // nolint: byted_returned_err_should_do_check

		}()

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return
			}

			if err != nil {
				_ = sw.Send(nil, err)
				return
			}

			msg, msgFound, e := resolveStreamResponse(resp)
			if e != nil {
				_ = sw.Send(nil, e)
				return
			}

			if !msgFound {
				continue
			}

			closed := sw.Send(&fmodel.CallbackOutput{
				Message:    msg,
				Config:     reqConf,
				TokenUsage: toModelCallbackUsage(msg.ResponseMeta),
			}, nil)
			if closed {
				return
			}
		}
	}()

	ctx, nsr := callbacks.OnEndWithStreamOutput(ctx, schema.StreamReaderWithConvert(sr,
		func(src *fmodel.CallbackOutput) (callbacks.CallbackOutput, error) {
			return src, nil
		}))

	outStream = schema.StreamReaderWithConvert(nsr,
		func(src callbacks.CallbackOutput) (*schema.Message, error) {
			s := src.(*fmodel.CallbackOutput)
			if s.Message == nil {
				return nil, schema.ErrNoValue
			}

			return s.Message, nil
		},
	)

	return outStream, nil
}

func (cm *ChatModel) genRequest(in []*schema.Message, options *fmodel.Options) (req *model.CreateChatCompletionRequest, err error) {
	req = &model.CreateChatCompletionRequest{
		MaxTokens:        options.MaxTokens,
		Temperature:      options.Temperature,
		TopP:             options.TopP,
		Model:            dereferenceOrZero(options.Model),
		Stop:             options.Stop,
		FrequencyPenalty: cm.config.FrequencyPenalty,
		LogitBias:        cm.config.LogitBias,
		PresencePenalty:  cm.config.PresencePenalty,
	}

	for _, msg := range in {
		content, e := toArkContent(msg.Content, msg.MultiContent)
		if e != nil {
			return req, e
		}

		req.Messages = append(req.Messages, &model.ChatCompletionMessage{
			Content:    content,
			Role:       string(msg.Role),
			ToolCallID: msg.ToolCallID,
			ToolCalls:  toArkToolCalls(msg.ToolCalls),
		})
	}

	tools := cm.tools
	if options.Tools != nil {
		if tools, err = toTools(options.Tools); err != nil {
			return nil, err
		}
	}

	if tools != nil {
		req.Tools = make([]*model.Tool, 0, len(tools))

		for _, tool := range cm.tools {
			arkTool := &model.Tool{
				Type: model.ToolTypeFunction,
				Function: &model.FunctionDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				},
			}

			req.Tools = append(req.Tools, arkTool)
		}
	}

	return req, nil
}

func (cm *ChatModel) resolveChatResponse(resp model.ChatCompletionResponse) (msg *schema.Message, err error) {
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	var choice *model.ChatCompletionChoice

	for _, c := range resp.Choices {
		if c.Index == 0 {
			choice = c
			break
		}
	}

	if choice == nil {
		return nil, fmt.Errorf("invalid response format: choice with index 0 not found")
	}

	content := choice.Message.Content
	if content == nil && len(choice.Message.ToolCalls) == 0 {
		return nil, fmt.Errorf("invalid response format: message has neither content nor tool calls")
	}

	msg = &schema.Message{
		Role:       schema.RoleType(choice.Message.Role),
		ToolCallID: choice.Message.ToolCallID,
		ToolCalls:  toMessageToolCalls(choice.Message.ToolCalls),
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: string(choice.FinishReason),
			Usage:        toEinoTokenUsage(&resp.Usage),
		},
		Extra: map[string]any{
			keyOfRequestID: arkRequestID(resp.ID),
		},
	}

	if content != nil && content.StringValue != nil {
		msg.Content = *content.StringValue
	}

	if choice.Message.ReasoningContent != nil {
		msg.Extra[keyOfReasoningContent] = *choice.Message.ReasoningContent
	}

	return msg, nil
}

func resolveStreamResponse(resp model.ChatCompletionStreamResponse) (msg *schema.Message, msgFound bool, err error) {
	if len(resp.Choices) > 0 {

		for _, choice := range resp.Choices {
			if choice.Index != 0 {
				continue
			}

			msgFound = true
			msg = &schema.Message{
				Role:      schema.RoleType(choice.Delta.Role),
				ToolCalls: toMessageToolCalls(choice.Delta.ToolCalls),
				Content:   choice.Delta.Content,
				ResponseMeta: &schema.ResponseMeta{
					FinishReason: string(choice.FinishReason),
					Usage:        toEinoTokenUsage(resp.Usage),
				},
				Extra: map[string]any{
					keyOfRequestID: arkRequestID(resp.ID),
				},
			}

			if choice.Delta.ReasoningContent != nil {
				msg.Extra[keyOfReasoningContent] = *choice.Delta.ReasoningContent
			}

			break
		}
	}

	if !msgFound && resp.Usage != nil {
		msgFound = true
		msg = &schema.Message{
			ResponseMeta: &schema.ResponseMeta{
				Usage: toEinoTokenUsage(resp.Usage),
			},
			Extra: map[string]any{
				keyOfRequestID: arkRequestID(resp.ID),
			},
		}
	}

	return msg, msgFound, nil
}

func (cm *ChatModel) GetType() string {
	return getType()
}

func (cm *ChatModel) IsCallbacksEnabled() bool {
	return true
}

func (cm *ChatModel) WithTools(tools []*schema.ToolInfo) (fmodel.ToolCallingChatModel, error) {
	if len(tools) == 0 {
		return nil, errors.New("no tools to bind")
	}
	artTools, err := toTools(tools)
	if err != nil {
		return nil, fmt.Errorf("convert to ark tools fail: %w", err)
	}

	ncm := *cm
	ncm.tools = artTools
	ncm.rawTools = tools
	return &ncm, nil
}

func (cm *ChatModel) BindTools(tools []*schema.ToolInfo) error {
	var err error
	if len(tools) == 0 {
		return errors.New("no tools to bind")
	}
	cm.tools, err = toTools(tools)
	if err != nil {
		return err
	}

	cm.rawTools = tools

	return nil
}

func toEinoTokenUsage(usage *model.Usage) *schema.TokenUsage {
	if usage == nil {
		return nil
	}
	return &schema.TokenUsage{
		CompletionTokens: usage.CompletionTokens,
		PromptTokens:     usage.PromptTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

func toModelCallbackUsage(respMeta *schema.ResponseMeta) *fmodel.TokenUsage {
	if respMeta == nil {
		return nil
	}
	usage := respMeta.Usage
	if usage == nil {
		return nil
	}
	return &fmodel.TokenUsage{
		CompletionTokens: usage.CompletionTokens,
		PromptTokens:     usage.PromptTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

func toMessageToolCalls(toolCalls []*model.ToolCall) []schema.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	ret := make([]schema.ToolCall, len(toolCalls))
	for i := range toolCalls {
		toolCall := toolCalls[i]
		ret[i] = schema.ToolCall{
			Index: toolCall.Index,
			ID:    toolCall.ID,
			Type:  string(toolCall.Type),
			Function: schema.FunctionCall{
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			},
		}
	}

	return ret
}

func toArkContent(content string, multiContent []schema.ChatMessagePart) (*model.ChatCompletionMessageContent, error) {
	if len(multiContent) == 0 {
		return &model.ChatCompletionMessageContent{StringValue: ptrOf(content)}, nil
	}

	parts := make([]*model.ChatCompletionMessageContentPart, 0, len(multiContent))

	for _, part := range multiContent {
		switch part.Type {
		case schema.ChatMessagePartTypeText:
			parts = append(parts, &model.ChatCompletionMessageContentPart{
				Type: model.ChatCompletionMessageContentPartTypeText,
				Text: part.Text,
			})
		case schema.ChatMessagePartTypeImageURL:
			if part.ImageURL == nil {
				return nil, fmt.Errorf("ImageURL field must not be nil when Type is ChatMessagePartTypeImageURL")
			}
			parts = append(parts, &model.ChatCompletionMessageContentPart{
				Type: model.ChatCompletionMessageContentPartTypeImageURL,
				ImageURL: &model.ChatMessageImageURL{
					URL:    part.ImageURL.URL,
					Detail: model.ImageURLDetail(part.ImageURL.Detail),
				},
			})
		default:
			return nil, fmt.Errorf("unsupported chat message part type: %s", part.Type)
		}
	}

	return &model.ChatCompletionMessageContent{
		ListValue: parts,
	}, nil
}

func toArkToolCalls(toolCalls []schema.ToolCall) []*model.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	ret := make([]*model.ToolCall, len(toolCalls))
	for i := range toolCalls {
		toolCall := toolCalls[i]
		ret[i] = &model.ToolCall{
			ID:   toolCall.ID,
			Type: model.ToolTypeFunction,
			Function: model.FunctionCall{
				Arguments: toolCall.Function.Arguments,
				Name:      toolCall.Function.Name,
			},
			Index: toolCall.Index,
		}
	}

	return ret
}

func toTools(tls []*schema.ToolInfo) ([]tool, error) {
	tools := make([]tool, len(tls))
	for i := range tls {
		ti := tls[i]
		if ti == nil {
			return nil, fmt.Errorf("tool info cannot be nil")
		}

		paramsJSONSchema, err := ti.ParamsOneOf.ToOpenAPIV3()
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool parameters to JSONSchema: %w", err)
		}

		tools[i] = tool{
			Function: &functionDefinition{
				Name:        ti.Name,
				Description: ti.Desc,
				Parameters:  paramsJSONSchema,
			},
		}
	}

	return tools, nil
}

func closeArkStreamReader(r *autils.ChatCompletionStreamReader) error {
	if r == nil || r.Response == nil || r.Response.Body == nil {
		return nil
	}

	return r.Close()
}

func ptrOf[T any](v T) *T {
	return &v
}

type panicErr struct {
	info  any
	stack []byte
}

func (p *panicErr) Error() string {
	return fmt.Sprintf("panic error: %v, \nstack: %s", p.info, string(p.stack))
}

func newPanicErr(info any, stack []byte) error {
	return &panicErr{
		info:  info,
		stack: stack,
	}
}

func convCompletionRequest(req *model.CreateChatCompletionRequest, contextID string) *model.ContextChatCompletionRequest {
	return &model.ContextChatCompletionRequest{
		ContextID:        contextID,
		Model:            req.Model,
		Messages:         req.Messages,
		MaxTokens:        dereferenceOrZero(req.MaxTokens),
		Temperature:      dereferenceOrZero(req.Temperature),
		TopP:             dereferenceOrZero(req.TopP),
		Stream:           dereferenceOrZero(req.Stream),
		Stop:             req.Stop,
		FrequencyPenalty: dereferenceOrZero(req.FrequencyPenalty),
		LogitBias:        req.LogitBias,
		LogProbs:         dereferenceOrZero(req.LogProbs),
		TopLogProbs:      dereferenceOrZero(req.TopLogProbs),
		User:             dereferenceOrZero(req.User),
		FunctionCall:     req.FunctionCall,
		Tools:            req.Tools,
		ToolChoice:       req.ToolChoice,
		StreamOptions:    req.StreamOptions,
	}
}
