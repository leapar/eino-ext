package reranker

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

type ReRanker interface {
	ReRankDocuments(ctx context.Context, texts []*schema.Document, query string) ([]*schema.Document, error) // invoke
}
