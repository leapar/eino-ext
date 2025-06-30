/*
 * Copyright 2025 CloudWeGo Authors
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

package put

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type PutRequest struct {
	URL  string `json:"url" jsonschema:"description=The URL to make the PUT request"`
	Body string `json:"body" jsonschema:"description=The body to send in the PUT request"`
}

func (r *PutRequestTool) Put(ctx context.Context, req *PutRequest) (string, error) {

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, req.URL, strings.NewReader(req.Body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	for key, value := range r.config.Headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}
