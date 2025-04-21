package main

import (
	"context"
	"log"

	"github.com/cloudwego/eino-ext/components/embedding/bailian"
)

func main() {
	ctx := context.Background()

	embedder, err := bailian.NewEmbedder(ctx, &bailian.EmbeddingConfig{
		APIKey: ALI_BAILIAN_API_KEY,
	})
	if err != nil {
		log.Printf("new embedder error: %v\n", err)
		return
	}

	texts := make([]map[string]string, 0)
	texts = append(texts, map[string]string{
		"text": "通用多模态表征模型",
	})
	texts = append(texts, map[string]string{
		"image": "https://mitalinlp.oss-cn-hangzhou.aliyuncs.com/dingkun/images/1712648554702.jpg",
	})
	texts = append(texts, map[string]string{
		"video": "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250107/lbcemt/new+video.mp4",
	})

	embedding, err := embedder.EmbedMultiModal(ctx, texts)
	if err != nil {
		log.Printf("embedding error: %v\n", err)
		return
	}

	log.Printf("embedding: %v\n", embedding)
}
