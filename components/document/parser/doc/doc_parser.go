package doc

import (
	"context"
	"fmt"
	"io"

	"code.sajari.com/docconv/v2"
	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
)

// DocParser reads from io.Reader and parse its content as plain text.
// Attention: This is in alpha stage, and may not support all doc use cases well enough.
// For example, it will not preserve whitespace and new line for now.
type DocParser struct {
}

// NewDocParser creates a new doc parser.
func NewDocParser() (*DocParser, error) {
	return &DocParser{}, nil
}

func (dp *DocParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) (docs []*schema.Document, err error) {
	text, metadata, err := docconv.ConvertDocx(reader)
	if err != nil {
		return nil, fmt.Errorf("doc donvert failed: %w", err)
	}

	document := &schema.Document{
		Content: text,
	}

	meta := make(map[string]any, 0)

	for k, v := range metadata {
		if len(k) == 0 {
			continue
		}
		meta[k] = v
	}

	option := parser.GetCommonOptions(&parser.Options{}, opts...)
	if option.ExtraMeta != nil {
		for k, v := range option.ExtraMeta {
			meta[k] = v
		}
	}

	document.MetaData = meta

	return []*schema.Document{
		document,
	}, nil
}
