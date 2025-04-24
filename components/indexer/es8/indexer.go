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

package es8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

type IndexerConfig struct {
	Client *elasticsearch.Client `json:"client"`

	Index string `json:"index"`
	// BatchSize controls max texts size for embedding.
	// Default is 5.
	BatchSize int `json:"batch_size"`
	// FieldMapping supports customize es fields from eino document.
	// Each key - FieldValue.Value from field2Value will be saved, and
	// vector of FieldValue.Value will be saved if FieldValue.EmbedKey is not empty.
	DocumentToFields func(ctx context.Context, doc *schema.Document) (field2Value map[string]FieldValue, err error)
	// Embedding vectorization method, must provide in two cases
	// 1. VectorFields contains fields except doc Content
	// 2. VectorFields contains doc Content and vector not provided in doc extra (see Document.Vector method)
	Embedding embedding.Embedder
}

type FieldValue struct {
	// Value original Value
	Value any
	// EmbedKey if set, Value will be vectorized and saved to es.
	// If Stringify method is provided, Embedding input text will be Stringify(Value).
	// If Stringify method not set, retriever will try to assert Value as string.
	EmbedKey string
	// Stringify converts Value to string
	Stringify func(val any) (string, error)
}

type Indexer struct {
	client *elasticsearch.Client
	config *IndexerConfig
}

func NewIndexer(_ context.Context, conf *IndexerConfig) (*Indexer, error) {
	if conf.Client == nil {
		return nil, fmt.Errorf("[NewIndexer] es client not provided")
	}

	if conf.DocumentToFields == nil {
		return nil, fmt.Errorf("[NewIndexer] DocumentToFields method not provided")
	}

	if conf.BatchSize == 0 {
		conf.BatchSize = defaultBatchSize
	}

	return &Indexer{
		client: conf.Client,
		config: conf,
	}, nil
}

func (i *Indexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, i.GetType(), components.ComponentOfIndexer)
	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	options := indexer.GetCommonOptions(&indexer.Options{
		Embedding: i.config.Embedding,
	}, opts...)

	if err = i.bulkAdd(ctx, docs, options); err != nil {
		return nil, err
	}

	ids = iter(docs, func(t *schema.Document) string { return t.ID })

	callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})

	return ids, nil
}

func (i *Indexer) bulkAdd(ctx context.Context, docs []*schema.Document, options *indexer.Options) error {
	emb := options.Embedding
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:  i.config.Index,
		Client: i.client,
	})
	if err != nil {
		return err
	}

	var (
		tuples []tuple
		texts  []string
	)

	embAndAdd := func() error {
		var vectors [][]float64

		if len(texts) > 0 {
			if emb == nil {
				return fmt.Errorf("[bulkAdd] embedding method not provided")
			}

			vectors, err = emb.EmbedStrings(i.makeEmbeddingCtx(ctx, emb), texts)
			if err != nil {
				return fmt.Errorf("[bulkAdd] embedding failed, %w", err)
			}

			if len(vectors) != len(texts) {
				return fmt.Errorf("[bulkAdd] invalid vector length, expected=%d, got=%d", len(texts), len(vectors))
			}
		}

		for _, t := range tuples {
			fields := t.fields
			for k, idx := range t.key2Idx {
				fields[k] = vectors[idx]
			}

			b, err := json.Marshal(fields)
			if err != nil {
				return fmt.Errorf("[bulkAdd] marshal bulk item failed, %w", err)
			}

			if err = bi.Add(ctx, esutil.BulkIndexerItem{
				Index:      i.config.Index,
				Action:     "index",
				DocumentID: t.id,
				Body:       bytes.NewReader(b),
			}); err != nil {
				return err
			}
		}

		tuples = tuples[:0]
		texts = texts[:0]

		return nil
	}

	for idx := range docs {
		doc := docs[idx]
		fields, err := i.config.DocumentToFields(ctx, doc)
		if err != nil {
			return fmt.Errorf("[bulkAdd] FieldMapping failed, %w", err)
		}

		rawFields := make(map[string]any)
		embSize := 0
		for k, v := range fields {
			rawFields[k] = v.Value
			if v.EmbedKey != "" {
				embSize++
			}
		}

		if embSize > i.config.BatchSize {
			return fmt.Errorf("[bulkAdd] needEmbeddingFields length over batch size, batch size=%d, got size=%d",
				i.config.BatchSize, embSize)
		}

		if len(texts)+embSize > i.config.BatchSize {
			if err = embAndAdd(); err != nil {
				return err
			}
		}

		key2Idx := make(map[string]int, embSize)
		for k, v := range fields {
			if v.EmbedKey != "" {
				if _, found := fields[v.EmbedKey]; found {
					return fmt.Errorf("[bulkAdd] duplicate key for origin key, key=%s", k)
				}

				if _, found := key2Idx[v.EmbedKey]; found {
					return fmt.Errorf("[bulkAdd] duplicate key from embed_key, key=%s", v.EmbedKey)
				}

				var text string
				if v.Stringify != nil {
					text, err = v.Stringify(v.Value)
					if err != nil {
						return err
					}
				} else {
					var ok bool
					text, ok = v.Value.(string)
					if !ok {
						return fmt.Errorf("[bulkAdd] assert value as string failed, key=%s, emb_key=%s", k, v.EmbedKey)
					}
				}

				key2Idx[v.EmbedKey] = len(texts)
				texts = append(texts, text)
			}
		}

		tuples = append(tuples, tuple{
			id:      doc.ID,
			fields:  rawFields,
			key2Idx: key2Idx,
		})
	}

	if len(tuples) > 0 {
		if err = embAndAdd(); err != nil {
			return err
		}
	}

	return bi.Close(ctx)
}

func (i *Indexer) makeEmbeddingCtx(ctx context.Context, emb embedding.Embedder) context.Context {
	runInfo := &callbacks.RunInfo{
		Component: components.ComponentOfEmbedding,
	}

	if embType, ok := components.GetType(emb); ok {
		runInfo.Type = embType
	}

	runInfo.Name = runInfo.Type + string(runInfo.Component)

	return callbacks.ReuseHandlers(ctx, runInfo)
}

func (i *Indexer) GetType() string {
	return typ
}

func (i *Indexer) IsCallbacksEnabled() bool {
	return true
}

type tuple struct {
	id      string
	fields  map[string]any
	key2Idx map[string]int
}
