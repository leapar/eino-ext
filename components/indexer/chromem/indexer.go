package chromem

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/philippgille/chromem-go"
)

type IndexerConfig struct {
	Client *chromem.DB

	StoragePath string `json:"storage_path"`

	Persistent bool   `json:"persistent"`
	Compress   bool   `json:"compress"`
	Collection string `json:"collection"`

	Embedding embedding.Embedder

	AddBatchSize int `json:"add_batch_size"`
}

type Indexer struct {
	config      *IndexerConfig
	collections map[string]*chromem.Collection
	mu          sync.RWMutex
	collection  *chromem.Collection
}

func NewIndexer(ctx context.Context, config *IndexerConfig) (*Indexer, error) {
	var err error
	if config.Embedding == nil {
		return nil, fmt.Errorf("[NewIndexer] embedding not provided for chromem indexer")
	}

	if len(config.Collection) == 0 {
		config.Collection = defaultCollection
	}

	if config.AddBatchSize == 0 {
		config.AddBatchSize = defaultAddBatchSize
	}

	if config.Client == nil {
		// 初始化DB
		if config.Persistent {
			config.Client, err = chromem.NewPersistentDB(config.StoragePath, config.Compress)
			if err != nil {
				return nil, fmt.Errorf("failed to create persistent vector DB: %w", err)
			}
		} else {
			config.Client = chromem.NewDB()
		}
	}

	i := &Indexer{
		config:      config,
		collections: make(map[string]*chromem.Collection),
		mu:          sync.RWMutex{},
	}
	collection, err := i.getOrCreateCollection(i.config.Collection)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}
	i.collection = collection
	return i, nil
}

func (i *Indexer) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) (ids []string, err error) {
	defer func() {
		if err != nil {
			ctx = callbacks.OnError(ctx, err)
		}
	}()

	options := indexer.GetCommonOptions(&indexer.Options{
		Embedding: i.config.Embedding,
	}, opts...)

	ctx = callbacks.OnStart(ctx, &indexer.CallbackInput{Docs: docs})

	ids = make([]string, 0, len(docs))
	for _, sub := range chunk(docs, i.config.AddBatchSize) {
		documents, err := i.convertDocuments(ctx, sub, options)
		if err != nil {
			return nil, fmt.Errorf("convertDocuments failed: %w", err)
		}

		//这个地方可以把content存入mysql获取MysqlDocId
		//然后从content内容改为MysqlDocId
		//同时metadata里面添加isStoredInMysql=true
		//MetaData里面已经包含了_source就是原始文件名
		if err = i.collection.AddDocuments(ctx, documents, i.config.AddBatchSize); err != nil {
			return nil, fmt.Errorf("AddDocuments failed: %w", err)
		}

		ids = append(ids, iter(sub, func(t *schema.Document) string { return t.ID })...)
	}

	ctx = callbacks.OnEnd(ctx, &indexer.CallbackOutput{IDs: ids})

	return ids, nil
}

func (i *Indexer) convertDocuments(ctx context.Context, docs []*schema.Document, options *indexer.Options) ([]chromem.Document, error) {
	queries := iter(docs, func(doc *schema.Document) string {
		return doc.Content
	})

	dense, err := i.customEmbedding(ctx, queries, options)
	if err != nil {
		return nil, err
	}

	documents := make([]chromem.Document, len(docs))
	for idx := range docs {
		doc := docs[idx]
		document := chromem.Document{}

		if doc.ID == "" {
			doc.ID = uuid.New().String()
		}

		document.ID = doc.ID
		document.Content = doc.Content
		document.Embedding = make([]float32, len(dense[idx]))
		for k, v := range dense[idx] {
			document.Embedding[k] = float32(v)
		}
		document.Metadata = map[string]string{}
		for k, v := range doc.MetaData {
			document.Metadata[k] = fmt.Sprintf("%s", v)
		}
		documents[idx] = document
	}

	return documents, nil
}

func (i *Indexer) customEmbedding(ctx context.Context, queries []string, options *indexer.Options) (vector [][]float64, err error) {
	emb := options.Embedding
	vectors, err := emb.EmbedStrings(i.makeEmbeddingCtx(ctx, emb), queries)
	if err != nil {
		return nil, err
	}

	if len(vectors) != len(queries) {
		return nil, fmt.Errorf("[customEmbedding] invalid return length of vector, got=%d, expected=%d", len(vectors), len(queries))
	}

	return vectors, nil
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

// 获取已存在的集合
func (i *Indexer) getCollection(name string) (*chromem.Collection, error) {
	i.mu.RLock()
	collection, exists := i.collections[name]
	i.mu.RUnlock()

	if exists {
		return collection, nil
	}

	// 尝试从数据库加载
	collections := i.config.Client.ListCollections()
	if coll, exists := collections[name]; exists {
		i.mu.Lock()
		i.collections[name] = coll
		i.mu.Unlock()
		return coll, nil
	}

	return nil, fmt.Errorf("collection %s not found", name)
}

// 获取或创建集合
func (i *Indexer) getOrCreateCollection(name string) (*chromem.Collection, error) {
	// 先尝试获取
	collection, err := i.getCollection(name)
	if err == nil {
		return collection, nil
	}

	// 创建新集合
	i.mu.Lock()
	defer i.mu.Unlock()

	// 再次检查，避免并发问题
	collections := i.config.Client.ListCollections()
	if coll, exists := collections[name]; exists {
		i.collections[name] = coll
		return coll, nil
	}

	// 创建集合
	coll, err := i.config.Client.GetOrCreateCollection(name, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection %s: %w", name, err)
	}

	i.collections[name] = coll
	return coll, nil
}

func (i *Indexer) GetType() string {
	return typ
}

func (i *Indexer) IsCallbacksEnabled() bool {
	return true
}
