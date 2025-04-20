package xlsx

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
	"github.com/xuri/excelize/v2"
)

// XlsxParser reads from io.Reader and parse its content as plain text.
// Attention: This is in alpha stage, and may not support all xlsx use cases well enough.
// For example, it will not preserve whitespace and new line for now.
type XlsxParser struct {
}

// NewXlsxParser creates a new xlsx parser.
func NewXlsxParser() (*XlsxParser, error) {
	return &XlsxParser{}, nil
}

func (xp *XlsxParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) (docs []*schema.Document, err error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	result := make([]*schema.Document, 0)
	for i, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			log.Println(err)
			continue
		}
		content := bytes.Buffer{}
		for _, row := range rows {
			content.WriteString(strings.Join(row, ","))
		}

		meta := make(map[string]any, 0)
		option := parser.GetCommonOptions(&parser.Options{}, opts...)
		if option.ExtraMeta != nil {
			for k, v := range option.ExtraMeta {
				meta[k] = v
			}
		}
		meta["_page"] = i
		meta["_sheetName"] = name
		result = append(result, &schema.Document{Content: content.String(), MetaData: meta})
	}
	return result, err
}
