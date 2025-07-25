package chromem

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"

	"github.com/cloudwego/eino-ext/components/reranker"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/philippgille/chromem-go"
)

type RetrieverConfig struct {
	Client *chromem.DB

	Collection string `json:"collection"`

	TopK           int     `json:"top_k,omitempty"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`

	Embedding embedding.Embedder
	ReRanker  reranker.ReRanker
}

type Retriever struct {
	config      *RetrieverConfig
	collections map[string]*chromem.Collection
	mu          sync.RWMutex
	collection  *chromem.Collection
}

func NewRetriever(ctx context.Context, config *RetrieverConfig) (*Retriever, error) {
	if config.Embedding == nil {
		return nil, fmt.Errorf("[NewRetriever] embedding not provided for chromem retriever")
	}
	var err error
	if config.Client == nil {
		config.Client, err = chromem.NewPersistentDB("", true)
		if err != nil {
			return nil, fmt.Errorf("failed to get db: %w", err)
		}
	}

	if config.TopK == 0 {
		config.TopK = defaultTopK
	}

	if len(config.Collection) == 0 {
		config.Collection = defaultCollection
	}

	r := &Retriever{
		config:      config,
		collections: make(map[string]*chromem.Collection),
		mu:          sync.RWMutex{},
	}

	collection, err := r.getCollection(r.config.Collection)

	if err != nil {
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}
	r.collection = collection
	return r, nil
}

// 获取已存在的集合
func (r *Retriever) getCollection(name string) (*chromem.Collection, error) {
	r.mu.RLock()
	collection, exists := r.collections[name]
	r.mu.RUnlock()

	if exists {
		return collection, nil
	}

	// 尝试从数据库加载
	collections := r.config.Client.ListCollections()
	if coll, exists := collections[name]; exists {
		r.mu.Lock()
		r.collections[name] = coll
		r.mu.Unlock()
		return coll, nil
	}

	return nil, fmt.Errorf("collection %s not found", name)
}

func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) (docs []*schema.Document, err error) {
	defer func() {
		if err != nil {
			ctx = callbacks.OnError(ctx, err)
		}
	}()

	options := retriever.GetCommonOptions(&retriever.Options{
		TopK:           &r.config.TopK,
		ScoreThreshold: &r.config.ScoreThreshold,
		Embedding:      r.config.Embedding,
	}, opts...)

	ctx = callbacks.OnStart(ctx, &retriever.CallbackInput{
		Query:          query,
		TopK:           r.config.TopK,
		ScoreThreshold: options.ScoreThreshold,
	})

	dense, err := r.customEmbedding(ctx, query, options)
	if err != nil {
		return nil, err
	}

	queryEmbedding := make([]float32, len(dense))
	for k, v := range dense {
		queryEmbedding[k] = float32(v)
	}

	result, err := r.collection.QueryEmbedding(ctx, queryEmbedding, int(math.Min(float64(r.collection.Count()), float64(*options.TopK))), nil, nil)
	if err != nil {
		return nil, err
	}

	docs = make([]*schema.Document, 0)
	for _, data := range result {
		doc, err := r.data2Document(data)
		if err != nil {
			return nil, err
		}

		docs = append(docs, doc)
	}

	filterDocs := make([]*schema.Document, 0)

	//排序
	if r.config.ReRanker != nil {
		docs, err = r.config.ReRanker.ReRankDocuments(ctx, docs, query)
		if err != nil {
			return nil, err
		}
	}

	//过滤
	if options.ScoreThreshold != nil {
		scoreThreshold := *options.ScoreThreshold
		for i := 0; i < len(docs); i++ {
			doc := docs[i]
			if scoreThreshold > 0 && float64(doc.Score()) < scoreThreshold {
				continue
			}
			filterDocs = append(filterDocs, doc)
		}

		docs = filterDocs
	}

	ctx = callbacks.OnEnd(ctx, &retriever.CallbackOutput{Docs: docs})

	return docs, nil
}

func (r *Retriever) customEmbedding(ctx context.Context, query string, options *retriever.Options) (vector []float64, err error) {
	emb := options.Embedding
	vectors, err := emb.EmbedStrings(r.makeEmbeddingCtx(ctx, emb), []string{query})
	if err != nil {
		return nil, err
	}

	if len(vectors) != 1 {
		return nil, fmt.Errorf("[customEmbedding] invalid return length of vector, got=%d, expected=1", len(vectors))
	}

	return vectors[0], nil
}

func (r *Retriever) makeEmbeddingCtx(ctx context.Context, emb embedding.Embedder) context.Context {
	runInfo := &callbacks.RunInfo{
		Component: components.ComponentOfEmbedding,
	}

	if embType, ok := components.GetType(emb); ok {
		runInfo.Type = embType
	}

	runInfo.Name = runInfo.Type + string(runInfo.Component)

	return callbacks.ReuseHandlers(ctx, runInfo)
}

func (r *Retriever) data2Document(data chromem.Result) (*schema.Document, error) {
	doc := &schema.Document{
		ID:       data.ID,
		Content:  data.Content,
		MetaData: map[string]any{},
	}

	doc.WithScore(float64(data.Similarity))
	for k, v := range data.Metadata {
		doc.MetaData[k] = v
	}
	return doc, nil
}

func (r *Retriever) GetType() string {
	return typ
}

func (r *Retriever) IsCallbacksEnabled() bool {
	return true
}
