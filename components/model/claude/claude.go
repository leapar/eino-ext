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

package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/cloudwego/eino/components"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var _ model.ToolCallingChatModel = (*ChatModel)(nil)

// NewChatModel creates a new Claude chat model instance
//
// Parameters:
//   - ctx: The context for the operation
//   - conf: Configuration for the Claude model
//
// Returns:
//   - model.ChatModel: A chat model interface implementation
//   - error: Any error that occurred during creation
//
// Example:
//
//	model, err := claude.NewChatModel(ctx, &claude.Config{
//	    APIKey: "your-api-key",
//	    Model:  "claude-3-opus-20240229",
//	    MaxTokens: 2000,
//	})
func NewChatModel(ctx context.Context, config *Config) (*ChatModel, error) {
	var cli *anthropic.Client
	if !config.ByBedrock {
		var opts []option.RequestOption
		if config.BaseURL != nil {
			opts = append(opts, option.WithBaseURL(*config.BaseURL), option.WithAPIKey(config.APIKey))
		} else {
			opts = append(opts, option.WithAPIKey(config.APIKey))
		}
		if config.HTTPClient != nil {
			opts = append(opts, option.WithHTTPClient(config.HTTPClient))
		}
		cli = anthropic.NewClient(opts...)
	} else {
		var opts []func(*awsConfig.LoadOptions) error
		if config.Region != "" {
			opts = append(opts, awsConfig.WithRegion(config.Region))
		}
		if config.SecretAccessKey != "" && config.AccessKey != "" {
			opts = append(opts, awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				config.AccessKey,
				config.SecretAccessKey,
				config.SessionToken,
			)))
		} else if config.Profile != "" {
			opts = append(opts, awsConfig.WithSharedConfigProfile(config.Profile))
		}

		if config.HTTPClient != nil {
			opts = append(opts, awsConfig.WithHTTPClient(config.HTTPClient))
		}
		cli = anthropic.NewClient(bedrock.WithLoadDefaultConfig(ctx, opts...))
	}
	return &ChatModel{
		cli:           cli,
		maxTokens:     config.MaxTokens,
		model:         config.Model,
		stopSequences: config.StopSequences,
		temperature:   config.Temperature,
		topK:          config.TopK,
		topP:          config.TopP,
	}, nil
}

// Config contains the configuration options for the Claude model
type Config struct {
	// ByBedrock indicates whether to use Bedrock Service
	// Required for Bedrock
	ByBedrock bool

	// AccessKey is your Bedrock API Access key
	// Obtain from: https://docs.aws.amazon.com/bedrock/latest/userguide/getting-started.html
	// Optional for Bedrock
	AccessKey string

	// SecretAccessKey is your Bedrock API Secret Access key
	// Obtain from: https://docs.aws.amazon.com/bedrock/latest/userguide/getting-started.html
	// Optional for Bedrock
	SecretAccessKey string

	// SessionToken is your Bedrock API Session Token
	// Obtain from: https://docs.aws.amazon.com/bedrock/latest/userguide/getting-started.html
	// Optional for Bedrock
	SessionToken string

	// Profile is your Bedrock API AWS profile
	// This parameter is ignored if AccessKey and SecretAccessKey are provided
	// Obtain from: https://docs.aws.amazon.com/bedrock/latest/userguide/getting-started.html
	// Optional for Bedrock
	Profile string

	// Region is your Bedrock API region
	// Obtain from: https://docs.aws.amazon.com/bedrock/latest/userguide/getting-started.html
	// Optional for Bedrock
	Region string

	// BaseURL is the custom API endpoint URL
	// Use this to specify a different API endpoint, e.g., for proxies or enterprise setups
	// Optional. Example: "https://custom-claude-api.example.com"
	BaseURL *string

	// APIKey is your Anthropic API key
	// Obtain from: https://console.anthropic.com/account/keys
	// Required
	APIKey string

	// Model specifies which Claude model to use
	// Required
	Model string

	// MaxTokens limits the maximum number of tokens in the response
	// Range: 1 to model's context length
	// Required. Example: 2000 for a medium-length response
	MaxTokens int

	// Temperature controls randomness in responses
	// Range: [0.0, 1.0], where 0.0 is more focused and 1.0 is more creative
	// Optional. Example: float32(0.7)
	Temperature *float32

	// TopP controls diversity via nucleus sampling
	// Range: [0.0, 1.0], where 1.0 disables nucleus sampling
	// Optional. Example: float32(0.95)
	TopP *float32

	// TopK controls diversity by limiting the top K tokens to sample from
	// Optional. Example: int32(40)
	TopK *int32

	// StopSequences specifies custom stop sequences
	// The model will stop generating when it encounters any of these sequences
	// Optional. Example: []string{"\n\nHuman:", "\n\nAssistant:"}
	StopSequences []string

	// HTTPClient specifies the client to send HTTP requests.
	HTTPClient *http.Client `json:"http_client"`
}

type ChatModel struct {
	cli *anthropic.Client

	maxTokens     int
	model         string
	stopSequences []string
	temperature   *float32
	topK          *int32
	topP          *float32
	tools         []anthropic.ToolParam
	origTools     []*schema.ToolInfo
	toolChoice    *schema.ToolChoice
}

func (cm *ChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (message *schema.Message, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, cm.GetType(), components.ComponentOfChatModel)
	ctx = callbacks.OnStart(ctx, cm.getCallbackInput(input, opts...))
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	param, err := cm.genMessageNewParams(input, opts...)
	if err != nil {
		return nil, err
	}
	resp, err := cm.cli.Messages.New(ctx, param)
	if err != nil {
		return nil, fmt.Errorf("create new message fail: %w", err)
	}
	message, err = convOutputMessage(resp)
	if err != nil {
		return nil, fmt.Errorf("convert response to schema message fail: %w", err)
	}
	callbacks.OnEnd(ctx, cm.getCallbackOutput(message))
	return message, nil
}

func (cm *ChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (result *schema.StreamReader[*schema.Message], err error) {
	ctx = callbacks.EnsureRunInfo(ctx, cm.GetType(), components.ComponentOfChatModel)
	ctx = callbacks.OnStart(ctx, cm.getCallbackInput(input, opts...))
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	param, err := cm.genMessageNewParams(input, opts...)
	if err != nil {
		return nil, err
	}
	stream := cm.cli.Messages.NewStreaming(ctx, param)

	sr, sw := schema.Pipe[*model.CallbackOutput](1)
	go func() {
		defer func() {
			panicErr := recover()

			if panicErr != nil {
				_ = sw.Send(nil, newPanicErr(panicErr, debug.Stack()))
			}
			stream.Close()
			sw.Close()
		}()
		var waitList []*schema.Message
		streamCtx := &streamContext{}
		for stream.Next() {
			message, err_ := convStreamEvent(stream.Current(), streamCtx)
			if err_ != nil {
				_ = sw.Send(nil, fmt.Errorf("convert response chunk to schema message fail: %w", err_))
				return
			}
			if message == nil {
				continue
			}
			if isMessageEmpty(message) {
				waitList = append(waitList, message)
				continue
			}
			if len(waitList) != 0 {
				message, err = schema.ConcatMessages(append(waitList, message))
				if err != nil {
					_ = sw.Send(nil, fmt.Errorf("concat empty message fail: %w", err))
					return
				}
				waitList = []*schema.Message{}
			}
			closed := sw.Send(cm.getCallbackOutput(message), nil)
			if closed {
				return
			}
		}
		if len(waitList) > 0 {
			message, err_ := schema.ConcatMessages(waitList)
			if err_ != nil {
				_ = sw.Send(nil, fmt.Errorf("concat empty message fail: %w", err_))
				return
			} else {
				closed := sw.Send(cm.getCallbackOutput(message), nil)
				if closed {
					return
				}
			}
		}
		if stream.Err() != nil {
			_ = sw.Send(nil, stream.Err())
		}
	}()
	_, sr = callbacks.OnEndWithStreamOutput(ctx, sr)
	return schema.StreamReaderWithConvert(sr, func(t *model.CallbackOutput) (*schema.Message, error) {
		return t.Message, nil
	}), nil
}

func (cm *ChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if len(tools) == 0 {
		return nil, errors.New("no tools to bind")
	}
	aTools, err := toAnthropicToolParam(tools)
	if err != nil {
		return nil, fmt.Errorf("to anthropic tool param fail: %w", err)
	}

	tc := schema.ToolChoiceAllowed
	ncm := *cm
	ncm.tools = aTools
	ncm.toolChoice = &tc
	ncm.origTools = tools
	return &ncm, nil
}

func (cm *ChatModel) BindTools(tools []*schema.ToolInfo) error {
	if len(tools) == 0 {
		return errors.New("no tools to bind")
	}
	result, err := toAnthropicToolParam(tools)
	if err != nil {
		return err
	}

	cm.tools = result
	cm.origTools = tools
	tc := schema.ToolChoiceAllowed
	cm.toolChoice = &tc
	return nil
}

func (cm *ChatModel) BindForcedTools(tools []*schema.ToolInfo) error {
	if len(tools) == 0 {
		return errors.New("no tools to bind")
	}
	result, err := toAnthropicToolParam(tools)
	if err != nil {
		return err
	}

	cm.tools = result
	cm.origTools = tools
	tc := schema.ToolChoiceForced
	cm.toolChoice = &tc
	return nil
}

func toAnthropicToolParam(tools []*schema.ToolInfo) ([]anthropic.ToolParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	result := make([]anthropic.ToolParam, 0, len(tools))
	for _, tool := range tools {
		s, err := tool.ToOpenAPIV3()
		if err != nil {
			return nil, fmt.Errorf("convert to openapi v3 schema fail: %w", err)
		}
		result = append(result, anthropic.ToolParam{
			Name:        anthropic.F(tool.Name),
			Description: anthropic.F(tool.Desc),
			InputSchema: anthropic.F[any](s),
		})
	}

	return result, nil
}

func (cm *ChatModel) genMessageNewParams(input []*schema.Message, opts ...model.Option) (anthropic.MessageNewParams, error) {
	if len(input) == 0 {
		return anthropic.MessageNewParams{}, fmt.Errorf("input is empty")
	}

	commonOptions := model.GetCommonOptions(&model.Options{
		Model:       &cm.model,
		Temperature: cm.temperature,
		MaxTokens:   &cm.maxTokens,
		TopP:        cm.topP,
		Stop:        cm.stopSequences,
		Tools:       nil,
		ToolChoice:  cm.toolChoice,
	}, opts...)
	claudeOptions := model.GetImplSpecificOptions(&options{TopK: cm.topK}, opts...)

	param := anthropic.MessageNewParams{}
	if commonOptions.Model != nil {
		param.Model = anthropic.F(*commonOptions.Model)
	}
	if commonOptions.MaxTokens != nil {
		param.MaxTokens = anthropic.F(int64(*commonOptions.MaxTokens))
	}
	if commonOptions.Temperature != nil {
		param.Temperature = anthropic.F(float64(*commonOptions.Temperature))
	}
	if commonOptions.TopP != nil {
		param.TopP = anthropic.F(float64(*commonOptions.TopP))
	}
	if len(commonOptions.Stop) > 0 {
		param.StopSequences = anthropic.F(commonOptions.Stop)
	}
	if claudeOptions.TopK != nil {
		param.TopK = anthropic.F(int64(*claudeOptions.TopK))
	}

	tools := cm.tools
	if commonOptions.Tools != nil {
		var err error
		if tools, err = toAnthropicToolParam(commonOptions.Tools); err != nil {
			return anthropic.MessageNewParams{}, err
		}
	}

	if len(tools) > 0 {
		param.Tools = anthropic.F(tools)
	}

	if commonOptions.ToolChoice != nil {
		switch *commonOptions.ToolChoice {
		case schema.ToolChoiceForbidden:
			param.Tools = anthropic.F([]anthropic.ToolParam{}) // act like forbid tools
		case schema.ToolChoiceAllowed:
			param.ToolChoice = anthropic.F(anthropic.ToolChoiceUnionParam(anthropic.ToolChoiceAutoParam{
				Type: anthropic.F(anthropic.ToolChoiceAutoTypeAuto),
			}))
		case schema.ToolChoiceForced:
			if len(tools) == 0 {
				return anthropic.MessageNewParams{}, fmt.Errorf("tool choice is forced but tool is not provided")
			} else if len(tools) == 1 {
				param.ToolChoice = anthropic.F(anthropic.ToolChoiceUnionParam(anthropic.ToolChoiceToolParam{
					Name: tools[0].Name,
					Type: anthropic.F(anthropic.ToolChoiceToolTypeTool),
				}))
			} else {
				param.ToolChoice = anthropic.F(anthropic.ToolChoiceUnionParam(anthropic.ToolChoiceAnyParam{
					Type: anthropic.F(anthropic.ToolChoiceAnyTypeAny),
				}))
			}
		default:
			return anthropic.MessageNewParams{}, fmt.Errorf("tool choice=%s not support", *commonOptions.ToolChoice)
		}
	}

	// Convert messages
	var systemTextBlocks []anthropic.TextBlockParam
	for len(input) > 1 && input[0].Role == schema.System {
		systemTextBlocks = append(systemTextBlocks, anthropic.NewTextBlock(input[0].Content))
		input = input[1:]
	}
	if len(systemTextBlocks) > 0 {
		param.System = anthropic.F(systemTextBlocks)
	}

	messages := make([]anthropic.MessageParam, 0, len(input))
	for _, msg := range input {
		message, err := convSchemaMessage(msg)
		if err != nil {
			return anthropic.MessageNewParams{}, fmt.Errorf("convert schema message fail: %w", err)
		}
		messages = append(messages, *message)
	}
	param.Messages = anthropic.F(messages)

	return param, nil
}

func (cm *ChatModel) getCallbackInput(input []*schema.Message, opts ...model.Option) *model.CallbackInput {
	result := &model.CallbackInput{
		Messages: input,
		Tools: model.GetCommonOptions(&model.Options{
			Tools: cm.origTools,
		}, opts...).Tools,
		Config: cm.getConfig(),
	}
	return result
}

func (cm *ChatModel) getCallbackOutput(output *schema.Message) *model.CallbackOutput {
	result := &model.CallbackOutput{
		Message: output,
		Config:  cm.getConfig(),
	}
	if output.ResponseMeta != nil && output.ResponseMeta.Usage != nil {
		result.TokenUsage = &model.TokenUsage{
			PromptTokens:     output.ResponseMeta.Usage.PromptTokens,
			CompletionTokens: output.ResponseMeta.Usage.CompletionTokens,
			TotalTokens:      output.ResponseMeta.Usage.TotalTokens,
		}
	}
	return result
}

func (cm *ChatModel) getConfig() *model.Config {
	result := &model.Config{
		Model:     cm.model,
		MaxTokens: cm.maxTokens,
		Stop:      cm.stopSequences,
	}
	if cm.temperature != nil {
		result.Temperature = *cm.temperature
	}
	if cm.topP != nil {
		result.TopP = *cm.topP
	}
	return result
}

func (cm *ChatModel) GetType() string {
	return "Claude"
}

func (cm *ChatModel) IsCallbacksEnabled() bool {
	return true
}

func convSchemaMessage(message *schema.Message) (*anthropic.MessageParam, error) {
	result := &anthropic.MessageParam{}
	if message.Role == schema.Assistant {
		result.Role = anthropic.F(anthropic.MessageParamRoleAssistant)
	} else {
		result.Role = anthropic.F(anthropic.MessageParamRoleUser)
	}
	var messageParams []anthropic.ContentBlockParamUnion
	if len(message.Content) > 0 {
		if len(message.ToolCallID) > 0 {
			messageParams = append(messageParams, anthropic.NewToolResultBlock(message.ToolCallID, message.Content, false))
		} else {
			messageParams = append(messageParams, anthropic.NewTextBlock(message.Content))
		}
	} else {
		for i := range message.MultiContent {
			switch message.MultiContent[i].Type {
			case schema.ChatMessagePartTypeText:
				messageParams = append(messageParams, anthropic.NewTextBlock(message.MultiContent[i].Text))
			case schema.ChatMessagePartTypeImageURL:
				if message.MultiContent[i].ImageURL == nil {
					continue
				}
				mediaType, data, err := convImageBase64(message.MultiContent[i].ImageURL.URL)
				if err != nil {
					return nil, fmt.Errorf("extract base64 image fail: %w", err)
				}
				messageParams = append(messageParams, anthropic.NewImageBlockBase64(mediaType, data))
			default:
				return nil, fmt.Errorf("anthropic message type not supported: %s", message.MultiContent[i].Type)
			}
		}
	}

	for i := range message.ToolCalls {
		messageParams = append(messageParams, anthropic.NewToolUseBlockParam(message.ToolCalls[i].ID, message.ToolCalls[i].Function.Name, json.RawMessage(message.ToolCalls[i].Function.Arguments)))
	}

	result.Content = anthropic.F(messageParams)

	return result, nil
}

func convOutputMessage(resp *anthropic.Message) (*schema.Message, error) {
	message := &schema.Message{
		Role: schema.Assistant,
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: string(resp.StopReason),
			Usage: &schema.TokenUsage{
				PromptTokens:     int(resp.Usage.InputTokens),
				CompletionTokens: int(resp.Usage.OutputTokens),
				TotalTokens:      int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
			},
		},
	}

	for _, item := range resp.Content {
		switch item.Type {
		case anthropic.ContentBlockTypeText:
			message.Content += item.Text
		case anthropic.ContentBlockTypeToolUse:
			message.ToolCalls = append(message.ToolCalls, schema.ToolCall{
				ID: item.ID,
				Function: schema.FunctionCall{
					Name:      item.Name,
					Arguments: string(item.Input),
				},
			})
		default:
			return nil, fmt.Errorf("unknown anthropic content block type: %s", item.Type)
		}
	}

	return message, nil
}

type streamContext struct {
	toolIndex *int
}

func convStreamEvent(event anthropic.MessageStreamEvent, streamCtx *streamContext) (*schema.Message, error) {
	result := &schema.Message{
		Role: schema.Assistant,
	}

	switch e := event.AsUnion().(type) {
	case anthropic.MessageStartEvent:
		return convOutputMessage(&e.Message)

	case anthropic.MessageDeltaEvent:
		result.ResponseMeta = &schema.ResponseMeta{
			FinishReason: string(e.Delta.StopReason),
			Usage: &schema.TokenUsage{
				CompletionTokens: int(e.Usage.OutputTokens),
			},
		}
		return result, nil

	case anthropic.MessageStopEvent, anthropic.ContentBlockStopEvent:
		return nil, nil
	case anthropic.ContentBlockStartEvent:
		content := &anthropic.ContentBlock{}
		err := content.UnmarshalJSON([]byte(e.ContentBlock.JSON.RawJSON()))
		if err != nil {
			return nil, fmt.Errorf("unmarshal content block start event fail: %w", err)
		}
		switch content.Type {
		case anthropic.ContentBlockTypeText:
			result.Content = content.Text
		case anthropic.ContentBlockTypeToolUse:
			num := 0
			if streamCtx.toolIndex != nil {
				num = *streamCtx.toolIndex + 1
			}
			streamCtx.toolIndex = &num

			arguments := string(content.Input)
			if arguments == "{}" {
				arguments = ""
			}
			result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
				Index: streamCtx.toolIndex,
				ID:    content.ID,
				Function: schema.FunctionCall{
					Name:      content.Name,
					Arguments: arguments,
				},
			})
		default:
			return nil, fmt.Errorf("unknown anthropic content block type: %s", content.Type)
		}
		return result, nil

	case anthropic.ContentBlockDeltaEvent:
		switch delta := e.Delta.AsUnion().(type) {
		case anthropic.TextDelta:
			result.Content = delta.Text

		case anthropic.InputJSONDelta:
			result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
				Index: streamCtx.toolIndex,
				Function: schema.FunctionCall{
					Arguments: delta.PartialJSON,
				},
			})
		}
		return result, nil

	default:
		return nil, fmt.Errorf("unknown stream event type: %T", e)
	}
}

func convImageBase64(data string) (string, string, error) {
	if !strings.HasPrefix(data, "data:") {
		return "", "", fmt.Errorf("invalid base64 image: %s", data)
	}
	contents := strings.SplitN(data[5:], ",", 2)
	if len(contents) != 2 {
		return "", "", fmt.Errorf("invalid base64 image: %s", data)
	}
	headParts := strings.Split(contents[0], ";")
	bBase64 := false
	for _, part := range headParts {
		if part == "base64" {
			bBase64 = true
		}
	}
	if !bBase64 {
		return "", "", fmt.Errorf("invalid base64 image: %s", data)
	}
	return headParts[0], contents[1], nil
}

func isMessageEmpty(message *schema.Message) bool {
	if len(message.Content) == 0 && len(message.ToolCalls) == 0 && len(message.MultiContent) == 0 {
		return true
	}
	return false
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
