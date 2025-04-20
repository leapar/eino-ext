package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
)

// CsvParser reads from io.Reader and parse its content as plain text.
// Attention: This is in alpha stage, and may not support all csv use cases well enough.
// For example, it will not preserve whitespace and new line for now.
type CsvParser struct {
	columns []string
}

// NewCsvParser creates a new csv parser.
func NewCsvParser(columns ...string) (*CsvParser, error) {
	return &CsvParser{columns: columns}, nil
}

func (cp *CsvParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) (docs []*schema.Document, err error) {
	var header []string
	var rown int

	rd := csv.NewReader(reader)
	for {
		row, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(header) == 0 {
			header = append(header, row...)
			continue
		}

		var content []string
		for i, value := range row {
			if len(cp.columns) > 0 &&
				!slices.Contains(cp.columns, header[i]) {
				continue
			}

			line := fmt.Sprintf("%s: %s", header[i], value)
			content = append(content, line)
		}

		rown++

		meta := make(map[string]any, 0)
		option := parser.GetCommonOptions(&parser.Options{}, opts...)
		if option.ExtraMeta != nil {
			for k, v := range option.ExtraMeta {
				meta[k] = v
			}
		}
		meta["row"] = rown
		docs = append(docs, &schema.Document{
			Content:  strings.Join(content, "\n"),
			MetaData: meta,
		})
	}

	return docs, nil
}
