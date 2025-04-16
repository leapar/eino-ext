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

package main

import (
	"context"
	"log"

	"github.com/cloudwego/eino-ext/components/embedding/ollama"
	"github.com/cloudwego/eino-ext/components/indexer/libsql"
)

func main() {
	ctx := context.Background()
	embedder, err := ollama.NewEmbedder(ctx, &ollama.EmbeddingConfig{
		Model: "nomic-embed-text:latest",
	})
	if err != nil {
		log.Printf("new embedder error: %v\n", err)
		return
	}

	embedding, err := embedder.EmbedStrings(ctx, []string{"水晶宫元宇宙制作平台"})
	if err != nil {
		log.Printf("embedding error: %v\n", err)
		return
	}

	sql, err := libsql.InitLibSqlDb(ctx, "./vector.db", "")
	if err != nil {
		log.Printf("new embedder error: %v\n", err)
		return
	}

	err = sql.Init(1024)
	if err != nil {
		log.Printf("embedding error: %v\n", err)
		return
	}

	doc := &libsql.Document{
		ID:        "111",
		Content:   "水晶宫元宇宙制作平台",
		Embedding: embedding[0],
	}
	sql.InsertChunk(doc)

	log.Printf("embedding:\n")
}
