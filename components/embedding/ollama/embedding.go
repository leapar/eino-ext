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

package ollama

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/ollama/ollama/api"
)

var (
	defaultBaseURL = "http://localhost:11434"
	defaultTimeout = 10 * time.Minute
)

type EmbeddingConfig struct {
	// Timeout specifies the maximum duration to wait for API responses
	// If HTTPClient is set, Timeout will not be used.
	// Optional. Default: no timeout
	Timeout *time.Duration `json:"timeout"`

	// HTTPClient specifies the client to send HTTP requests.
	// If HTTPClient is set, Timeout will not be used.
	// Optional. Default &http.Client{Timeout: Timeout}
	HTTPClient *http.Client `json:"http_client"`

	BaseURL string `json:"base_url"`

	// Model specifies the ID of the model to use for embedding generation
	// Required
	Model string `json:"model"`
}

var _ embedding.Embedder = (*Embedder)(nil)

type Embedder struct {
	cli  *api.Client
	conf *EmbeddingConfig
}

func NewEmbedder(ctx context.Context, config *EmbeddingConfig) (*Embedder, error) {
	var httpClient *http.Client

	if len(config.BaseURL) == 0 {
		config.BaseURL = defaultBaseURL
	}

	if config.Timeout == nil {
		config.Timeout = &defaultTimeout
	}

	if config.HTTPClient != nil {
		httpClient = config.HTTPClient
	} else {
		httpClient = &http.Client{Timeout: *config.Timeout}
	}

	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	cli := api.NewClient(baseURL, httpClient)

	return &Embedder{
		cli:  cli,
		conf: config,
	}, nil
}

func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) (
	embeddings [][]float64, err error) {
	req := &api.EmbedRequest{
		Model: e.conf.Model,
		Input: texts,
	}
	resp, err := e.cli.Embed(ctx, req)
	if err != nil {
		return nil, err
	}

	embeddings = make([][]float64, len(resp.Embeddings))
	for i, embedding := range resp.Embeddings {
		res := make([]float64, len(embedding))
		for j, emb := range embedding {
			res[j] = float64(emb)
		}
		embeddings[i] = res
	}

	return embeddings, nil
}

const typ = "Ollama"

func (e *Embedder) GetType() string {
	return typ
}

func (e *Embedder) IsCallbacksEnabled() bool {
	return true
}
